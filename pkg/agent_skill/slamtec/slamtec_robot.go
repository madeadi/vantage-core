package slamtec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vantageos-core/pkg/agentskill"
	"vantageos-core/pkg/agentskill/slamtec/action"
)

const (
	defaultMoveMode       = 2
	actionPollInterval    = 250 * time.Millisecond
	actionTimeout         = 5 * time.Minute
	jackPollInterval      = 500 * time.Millisecond
	jackTimeout           = 60 * time.Second
	backgroundPollDelay   = time.Second
	backgroundPollTick    = 500 * time.Millisecond
	floorPollInitialDelay = 5 * time.Second
	maxRelevantEvents     = 5
)

// SlamtecRobot is a Go implementation of the Slamtec robot HTTP driver.
// It implements GoToSkill, StatusSkill, LocaliseSkill, RobotPoseSkill,
// MoveSkill, DeviceSkill, ChargingSkill, MaintenanceModeSkill,
// RobotStatusSkill, MapSkill, IdleMediaSkill, SoundSkill,
// EmergencyBrakeSkill, SoftBrakeSkill, LiftTakingSkill,
// JackUpDownSkill, AdvancedJackUpDownSkill, MoveToTagSkill, Initialize.
type SlamtecRobot struct {
	baseURL    string
	httpClient *http.Client

	layoutID   string
	layoutIDMu sync.RWMutex

	currentFloor   SlamtecFloor
	currentFloorMu sync.RWMutex

	robotPose   agentskill.RobotPose
	robotPoseMu sync.RWMutex

	pois   []SlamtecPoiSingleFloor
	poisMu sync.RWMutex

	cachedHealth RobotHealth
	healthMu     sync.RWMutex

	cachedPower PowerStatus
	powerMu     sync.RWMutex

	softBrakeActive atomic.Bool

	brakeState  atomic.Bool
	brakeSubs   []chan bool
	brakeSubsMu sync.Mutex
}

// New creates a SlamtecRobot and begins background polling loops once the
// HTTP endpoint is reachable.
func New(address, port string) *SlamtecRobot {
	r := &SlamtecRobot{
		baseURL:    strings.TrimRight(address, "/") + ":" + port,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	go func() {
		if !r.isReachable() {
			slog.Warn("SlamtecRobot: HTTP unreachable, background loops not started", "baseURL", r.baseURL)
			return
		}
		slog.Info("SlamtecRobot: connected", "baseURL", r.baseURL)
		r.refreshPois()
		go r.runFloorLoop()
		go r.runHealthLoop()
		go r.runPowerLoop()
		go r.runPoseLoop()
	}()
	return r
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (r *SlamtecRobot) get(path string, out any) error {
	resp, err := r.httpClient.Get(r.baseURL + path)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (r *SlamtecRobot) post(path string, body any, out any) error {
	return r.doJSON(http.MethodPost, path, body, out)
}

func (r *SlamtecRobot) put(path string, body any) error {
	return r.doJSON(http.MethodPut, path, body, nil)
}

func (r *SlamtecRobot) deleteReq(path string) error {
	req, err := http.NewRequest(http.MethodDelete, r.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("DELETE %s: HTTP %d: %s", path, resp.StatusCode, body)
	}
	return nil
}

func (r *SlamtecRobot) doJSON(method, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%s %s: marshal: %w", method, path, err)
	}
	req, err := http.NewRequest(method, r.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, b)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (r *SlamtecRobot) postRaw(path string, contentType string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, r.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, b)
	}
	return nil
}

func (r *SlamtecRobot) isReachable() bool {
	var ps PowerStatus
	if err := r.get("/api/core/system/v1/power/status", &ps); err != nil {
		slog.Warn("SlamtecRobot: connectivity check failed", "err", err)
		return false
	}
	return true
}

// ── Background loops ──────────────────────────────────────────────────────────

func (r *SlamtecRobot) runFloorLoop() {
	time.Sleep(floorPollInitialDelay)
	ticker := time.NewTicker(backgroundPollTick)
	defer ticker.Stop()
	for range ticker.C {
		var floor SlamtecFloor
		if err := r.get("/api/multi-floor/map/v1/floors/:current", &floor); err != nil {
			slog.Error("floor poll failed", "err", err)
			continue
		}
		r.currentFloorMu.Lock()
		r.currentFloor = floor
		r.currentFloorMu.Unlock()
	}
}

func (r *SlamtecRobot) runHealthLoop() {
	time.Sleep(backgroundPollDelay)
	ticker := time.NewTicker(backgroundPollTick)
	defer ticker.Stop()
	for range ticker.C {
		var health RobotHealth
		if err := r.get("/api/core/system/v1/robot/health", &health); err != nil {
			slog.Error("health poll failed", "err", err)
			continue
		}
		r.healthMu.Lock()
		r.cachedHealth = health
		r.healthMu.Unlock()

		newBrake := health.HasSystemEmergencyStop
		if r.brakeState.Swap(newBrake) != newBrake {
			r.broadcastBrake(newBrake)
		}
	}
}

func (r *SlamtecRobot) runPowerLoop() {
	time.Sleep(backgroundPollDelay)
	ticker := time.NewTicker(backgroundPollTick)
	defer ticker.Stop()
	for range ticker.C {
		var ps PowerStatus
		if err := r.get("/api/core/system/v1/power/status", &ps); err != nil {
			slog.Error("power poll failed", "err", err)
			continue
		}
		r.powerMu.Lock()
		r.cachedPower = ps
		r.powerMu.Unlock()
	}
}

func (r *SlamtecRobot) runPoseLoop() {
	ticker := time.NewTicker(backgroundPollTick)
	defer ticker.Stop()
	for range ticker.C {
		var p Pose
		if err := r.get("/api/core/slam/v1/localization/pose", &p); err != nil {
			slog.Warn("pose poll failed", "err", err)
			continue
		}
		r.robotPoseMu.Lock()
		r.robotPose = agentskill.RobotPose{X: p.X, Y: p.Y, Yaw: p.Yaw}
		r.robotPoseMu.Unlock()
	}
}

func (r *SlamtecRobot) broadcastBrake(state bool) {
	r.brakeSubsMu.Lock()
	defer r.brakeSubsMu.Unlock()
	for _, ch := range r.brakeSubs {
		select {
		case ch <- state:
		default:
		}
	}
}

// ── Action helpers ────────────────────────────────────────────────────────────

// submitAction POSTs an action and polls until it completes or ctx is cancelled.
func (r *SlamtecRobot) submitAction(ctx context.Context, payload any) error {
	var info ActionInfo
	if err := r.post("/api/core/motion/v1/actions", payload, &info); err != nil {
		return fmt.Errorf("create action: %w", err)
	}
	slog.Info("action created", "actionId", info.ActionID, "actionName", info.ActionName, "result", info.State.Result)
	if info.State.Result != actionResultSuccess {
		return fmt.Errorf("action rejected (result=%d reason=%s)", info.State.Result, info.State.Reason)
	}
	return r.waitForAction(ctx, info)
}

// waitForAction polls the action status until STATUS_DONE or timeout/cancel.
func (r *SlamtecRobot) waitForAction(ctx context.Context, started ActionInfo) error {
	ticker := time.NewTicker(actionPollInterval)
	defer ticker.Stop()
	deadline := time.After(actionTimeout)
	var lastDesc string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("action %d timed out after %v", started.ActionID, actionTimeout)
		case <-ticker.C:
			var info ActionInfo
			path := fmt.Sprintf("/api/core/motion/v1/actions/%d", started.ActionID)
			if err := r.get(path, &info); err != nil {
				slog.Error("getActionInfo failed", "actionId", started.ActionID, "err", err)
				continue
			}
			desc := fmt.Sprintf("id=%d name=%s stage=%s status=%d result=%d reason=%s",
				info.ActionID, info.ActionName, info.Stage, info.State.Status, info.State.Result, info.State.Reason)
			if desc != lastDesc {
				slog.Info("action state changed", "action", desc)
				lastDesc = desc
			}
			if info.State.Status == actionStatusDone {
				if info.State.Result == actionResultSuccess {
					return nil
				}
				diag := r.buildFailureDiagnostics(info, started)
				return fmt.Errorf("action %d failed: reason=%s diag=%s", started.ActionID, info.State.Reason, diag)
			}
			if info.State.Result < 0 {
				diag := r.buildFailureDiagnostics(info, started)
				return fmt.Errorf("action %d failed early: reason=%s diag=%s", started.ActionID, info.State.Reason, diag)
			}
		}
	}
}

func (r *SlamtecRobot) pollJackStatus(ctx context.Context, targetStage int, tag string) error {
	ticker := time.NewTicker(jackPollInterval)
	defer ticker.Stop()
	deadline := time.After(jackTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("%s: timed out waiting for jack stage=%d", tag, targetStage)
		case <-ticker.C:
			var status JackStatus
			if err := r.get("/api/core/system/v1/jack/status", &status); err != nil {
				slog.Error("getJackStatus failed", "tag", tag, "err", err)
				continue
			}
			slog.Debug("jack status", "tag", tag, "stage", status.Stage, "alarm", status.Alarm)
			if status.Alarm != 0 {
				return fmt.Errorf("%s: jack alarm=%d", tag, status.Alarm)
			}
			if status.Stage == targetStage {
				slog.Info("jack reached target stage", "tag", tag, "stage", targetStage)
				return nil
			}
		}
	}
}

func (r *SlamtecRobot) sendJackCommand(ctx context.Context, cmd jackStatusValue, tag string) error {
	body := []byte(`"` + string(cmd) + `"`)
	if err := r.postRaw("/api/core/system/v1/jack/status", "application/json", body); err != nil {
		return fmt.Errorf("%s: sendJackCommand(%s): %w", tag, cmd, err)
	}
	slog.Info("jack command sent", "tag", tag, "cmd", cmd)
	return nil
}

// ── Diagnostic helpers ────────────────────────────────────────────────────────

func (r *SlamtecRobot) buildFailureDiagnostics(info, started ActionInfo) string {
	r.robotPoseMu.RLock()
	pose := r.robotPose
	r.robotPoseMu.RUnlock()
	r.healthMu.RLock()
	health := r.cachedHealth
	r.healthMu.RUnlock()
	r.powerMu.RLock()
	power := r.cachedPower
	r.powerMu.RUnlock()
	r.currentFloorMu.RLock()
	floor := r.currentFloor
	r.currentFloorMu.RUnlock()

	r.layoutIDMu.RLock()
	layoutID := r.layoutID
	r.layoutIDMu.RUnlock()

	reason := info.State.Reason
	reasonHint := r.describeFailureReason(info.ActionName, reason)

	return fmt.Sprintf(
		"action=%s reasonHint=%s layoutId=%s building=%s floor=%s pose={%.3f,%.3f,%.3f} "+
			"battery=%d charging=%v docked=%v softBrake=%v "+
			"health={error=%v fatal=%v estop=%v lidarDiscon=%v} "+
			"dock=%s recentEvents=%s",
		info.ActionName, reasonHint, layoutID,
		floor.Building, floor.Floor,
		pose.X, pose.Y, pose.Yaw,
		power.BatteryPercentage, power.IsCharging,
		power.DockingStatus == DockingStatusDocked,
		r.softBrakeActive.Load(),
		health.HasError, health.HasFatal,
		health.HasSystemEmergencyStop, health.HasLidarDisconnected,
		r.summarizeCurrentDock(),
		r.summarizeRecentEvents(),
	)
}

func (r *SlamtecRobot) describeFailureReason(actionName, reason string) string {
	switch reason {
	case EventMoveToLandingPointFailed:
		return "could not navigate to dock landing point"
	case EventSearchDockFailed:
		return "reached dock area but could not detect charging dock"
	case EventChargingBaseFailed:
		return "failed during final docking or charging-base engagement"
	case EventDockIDNotFound:
		return "no valid home dock bound for active map/floor"
	case EventCurrentPoseOccupied:
		return "current pose occupied, motion planning blocked"
	case EventCliffDetected:
		return "cliff sensor interrupted movement"
	case EventBumperTriggered:
		return "bumper contact during approach"
	default:
		if strings.Contains(actionName, "GoHomeAction") || strings.Contains(actionName, "BackHomeAction") {
			return "docking action failed; check dock binding and reachability"
		}
		if reason == "" {
			return "<none>"
		}
		return reason
	}
}

// ── Initialize ────────────────────────────────────────────────────────────────

func (r *SlamtecRobot) InitRobot() {
	slog.Info("InitRobot: loading map")
	if err := r.get("/api/core/slam/v1/maps/explore", nil); err != nil {
		slog.Error("InitRobot: failed to fetch map", "err", err)
		return
	}
	slog.Info("InitRobot: map loaded")
}

func (r *SlamtecRobot) SetDefaultLayoutID(layoutID string) {
	slog.Info("SetDefaultLayoutID", "layoutId", layoutID)
	r.layoutIDMu.Lock()
	r.layoutID = layoutID
	r.layoutIDMu.Unlock()
}

func (r *SlamtecRobot) GetDefaultLayoutID() string {
	r.layoutIDMu.RLock()
	defer r.layoutIDMu.RUnlock()
	return r.layoutID
}

// ── StatusSkill ───────────────────────────────────────────────────────────────

func (r *SlamtecRobot) GetBatteryPercentage() int {
	r.powerMu.RLock()
	defer r.powerMu.RUnlock()
	return r.cachedPower.BatteryPercentage
}

func (r *SlamtecRobot) GetBatteryVoltage() float64 { return 0 }

func (r *SlamtecRobot) GetEstimatedRuntime() int { return 0 }

func (r *SlamtecRobot) GetEstimatedChargetime() int { return 0 }

func (r *SlamtecRobot) IsBatteryCharging() bool {
	r.powerMu.RLock()
	defer r.powerMu.RUnlock()
	return r.cachedPower.IsCharging || r.cachedPower.DockingStatus == DockingStatusDocked
}

func (r *SlamtecRobot) GetSignalStrength() int {
	var ns NetworkStatus
	if err := r.get("/api/core/system/v1/network/status", &ns); err != nil {
		slog.Error("GetSignalStrength failed", "err", err)
		return 0
	}
	return ns.Quality
}

func (r *SlamtecRobot) IsMainTaskExecuting() bool { return false }

// ── RobotStatusSkill ──────────────────────────────────────────────────────────

func (r *SlamtecRobot) GetRobotStatuses() []map[string]bool {
	statuses := []map[string]bool{
		{"SOFT_BRAKE": r.IsStatusActive("SOFT_BRAKE")},
		{"ESTOP": r.IsStatusActive("ESTOP")},
		{"FATAL": r.IsStatusActive("FATAL")},
		{"CHARGING": r.IsStatusActive("CHARGING")},
		{"DOCKED": r.IsStatusActive("DOCKED")},
		{"JACK_UP": r.IsStatusActive("JACK_UP")},
	}
	return statuses
}

func (r *SlamtecRobot) IsStatusActive(key string) bool {
	switch key {
	case "SOFT_BRAKE":
		return r.softBrakeActive.Load()
	case "ESTOP":
		r.healthMu.RLock()
		defer r.healthMu.RUnlock()
		return r.cachedHealth.HasSystemEmergencyStop
	case "FATAL":
		r.healthMu.RLock()
		defer r.healthMu.RUnlock()
		return r.cachedHealth.HasFatal
	case "CHARGING":
		r.powerMu.RLock()
		defer r.powerMu.RUnlock()
		return r.cachedPower.IsCharging || r.cachedPower.DockingStatus == DockingStatusDocked
	case "DOCKED":
		r.powerMu.RLock()
		defer r.powerMu.RUnlock()
		return r.cachedPower.DockingStatus == DockingStatusDocked
	case "JACK_UP":
		var status JackStatus
		if err := r.get("/api/core/system/v1/jack/status", &status); err != nil {
			slog.Error("IsStatusActive JACK_UP: failed", "err", err)
			return false
		}
		return status.Stage == 5
	default:
		slog.Debug("IsStatusActive: no mapping", "key", key)
		return false
	}
}

// ── RobotPoseSkill ────────────────────────────────────────────────────────────

func (r *SlamtecRobot) GetRobotPose() agentskill.RobotPose {
	r.robotPoseMu.RLock()
	defer r.robotPoseMu.RUnlock()
	return r.robotPose
}

// ── Current floor helpers ─────────────────────────────────────────────────────

func (r *SlamtecRobot) getBuilding() string {
	r.currentFloorMu.RLock()
	defer r.currentFloorMu.RUnlock()
	return r.currentFloor.Building
}

func (r *SlamtecRobot) getFloor() string {
	r.currentFloorMu.RLock()
	defer r.currentFloorMu.RUnlock()
	return r.currentFloor.Floor
}

// ── GoToSkill ─────────────────────────────────────────────────────────────────

func (r *SlamtecRobot) GoToNamedTarget(ctx context.Context, namedTarget string, opts agentskill.GoToOption) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)

		r.poisMu.RLock()
		var found *SlamtecPoiSingleFloor
		for i := range r.pois {
			if r.pois[i].Metadata.Name == namedTarget {
				found = &r.pois[i]
				break
			}
		}
		r.poisMu.RUnlock()

		if found == nil {
			slog.Error("GoToNamedTarget: POI not found", "name", namedTarget)
			sendResult(ch, agentskill.Result{Err: fmt.Errorf("POI %q not found", namedTarget), Status: agentskill.Failed})
			return
		}

		payload := action.NewMoveToActionFull(
			r.getBuilding(), r.getFloor(),
			float64(found.Pose.X), float64(found.Pose.Y), float64(found.Pose.Yaw),
		)
		mode := defaultMoveMode
		if !opts.Forward {
			mode = 4 // backward
		}
		if opts.MoveMode > 0 {
			mode = opts.MoveMode
		}
		payload.Options.MoveOptions.Mode = mode

		slog.Info("GoToNamedTarget", "name", namedTarget, "x", found.Pose.X, "y", found.Pose.Y, "mode", mode)
		if err := r.submitAction(ctx, payload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) GoToXY(ctx context.Context, x, y, yaw float64) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		var payload *action.MoveToAction
		if yaw != 0 {
			payload = action.NewMoveToActionXYYaw(x, y, yaw)
		} else {
			payload = action.NewMoveToActionXY(x, y)
		}
		payload.Options.MoveOptions.Mode = defaultMoveMode
		slog.Info("GoToXY", "x", x, "y", y, "yaw", yaw)
		if err := r.submitAction(ctx, payload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) StopGo(ctx context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("StopGo: aborting current action")
		if err := r.deleteReq("/api/core/motion/v1/actions/:current"); err != nil {
			slog.Error("StopGo failed", "err", err)
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) refreshPois() {
	var pois []SlamtecPoiSingleFloor
	if err := r.get("/api/core/artifact/v1/pois", &pois); err != nil {
		slog.Error("refreshPois failed", "err", err)
		return
	}
	r.poisMu.Lock()
	r.pois = pois
	r.poisMu.Unlock()
	slog.Info("POIs loaded", "count", len(pois))
}

// ── ChargingSkill ─────────────────────────────────────────────────────────────

func (r *SlamtecRobot) GoChargeNearby(ctx context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("GoChargeNearby: requesting home/charge dock")
		payload := action.NewGoHomeActionOption()
		if err := r.submitAction(ctx, payload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

// ── LocaliseSkill ─────────────────────────────────────────────────────────────

func (r *SlamtecRobot) AutoLocalise(ctx context.Context, layoutID, layoutName, buildingName string) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		r.layoutIDMu.Lock()
		r.layoutID = layoutID
		r.layoutIDMu.Unlock()
		slog.Info("AutoLocalise", "layoutId", layoutID, "layoutName", layoutName, "building", buildingName)
		if buildingName != "" && layoutName != "" {
			if err := r.put("/api/multi-floor/map/v1/floors/:current", updateFloor{Building: buildingName, Floor: layoutName}); err != nil {
				slog.Error("AutoLocalise: updateFloor failed", "err", err)
				sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
				return
			}
		}
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			sendResult(ch, agentskill.Result{Err: ctx.Err(), Status: agentskill.Cancelled})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) ManualLocalise(ctx context.Context, layoutID, layoutName, buildingName string, pose agentskill.RobotPose) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		r.layoutIDMu.Lock()
		r.layoutID = layoutID
		r.layoutIDMu.Unlock()
		slog.Info("ManualLocalise", "layoutId", layoutID, "layoutName", layoutName, "building", buildingName)
		if buildingName != "" && layoutName != "" {
			p := &Pose{X: pose.X, Y: pose.Y, Yaw: pose.Yaw}
			payload := updateFloor{Building: buildingName, Floor: layoutName, Pose: p}
			if err := r.put("/api/multi-floor/map/v1/floors/:current", payload); err != nil {
				slog.Error("ManualLocalise: updateFloor failed", "err", err)
				sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
				return
			}
		}
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			sendResult(ch, agentskill.Result{Err: ctx.Err(), Status: agentskill.Cancelled})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

// ── MapSkill ──────────────────────────────────────────────────────────────────

func (r *SlamtecRobot) ChangeMap(_ context.Context, newMap string) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("ChangeMap", "newMap", newMap)
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

// ── ZoneSkill ─────────────────────────────────────────────────────────────────

func (r *SlamtecRobot) SyncZones() {
	slog.Info("SyncZones: not implemented")
}

// ── MaintenanceModeSkill ──────────────────────────────────────────────────────

func (r *SlamtecRobot) EnableMaintenance(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("EnableMaintenance")
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) DisableMaintenance(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("DisableMaintenance")
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

// ── MoveSkill ─────────────────────────────────────────────────────────────────

func (r *SlamtecRobot) MoveForward(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Debug("MoveForward")
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) MoveBackward(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Debug("MoveBackward")
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) TurnLeft(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Debug("TurnLeft")
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) TurnRight(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Debug("TurnRight")
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) StopMoving(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("StopMoving")
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) Move(_ context.Context, linearVel, angularVel float64) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Debug("Move", "linear", linearVel, "angular", angularVel)
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

// ── DeviceSkill ───────────────────────────────────────────────────────────────

func (r *SlamtecRobot) SendToDevice(_ context.Context, device agentskill.Device, payload map[string]any) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("SendToDevice", "device", device.ID, "payload", payload)
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) GetDeviceStatus(_ agentskill.Device) map[string]any {
	return map[string]any{}
}

// ── IdleMediaSkill ────────────────────────────────────────────────────────────

func (r *SlamtecRobot) DisplayIdleMediaSequence(seq agentskill.IdleMediaSequence) {
	slog.Info("DisplayIdleMediaSequence", "items", seq.Items)
}

// ── SoundSkill ────────────────────────────────────────────────────────────────

func (r *SlamtecRobot) PlayMediaSound(_ context.Context, media string) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("PlayMediaSound", "media", media)
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) PlayTextSound(_ context.Context, text string) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("PlayTextSound", "text", text)
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) ResetSound(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("ResetSound")
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) GetSoundStatus() agentskill.SoundStatus {
	return agentskill.SoundOff
}

func (r *SlamtecRobot) WhatsPlayingOnSound() string { return "" }

// ── EmergencyBrakeSkill ───────────────────────────────────────────────────────

func (r *SlamtecRobot) BrakeCh() <-chan bool {
	ch := make(chan bool, 1)
	// Register before seeding: broadcastBrake holds brakeSubsMu, so holding
	// the lock across both append and seed prevents a missed transition.
	r.brakeSubsMu.Lock()
	r.brakeSubs = append(r.brakeSubs, ch)
	ch <- r.brakeState.Load()
	r.brakeSubsMu.Unlock()
	return ch
}

func (r *SlamtecRobot) IsBrakeOn() bool {
	return r.brakeState.Load()
}

// ── SoftBrakeSkill ────────────────────────────────────────────────────────────

func (r *SlamtecRobot) ActivateSoftBrake(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("ActivateSoftBrake: triggering emergency stop")
		if err := r.put("/api/core/system/v1/parameter", paramEmergencyStopOn()); err != nil {
			slog.Error("ActivateSoftBrake failed", "err", err)
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		r.softBrakeActive.Store(true)
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) ReleaseSoftBrake(_ context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("ReleaseSoftBrake: releasing emergency stop")
		if err := r.put("/api/core/system/v1/parameter", paramEmergencyStopOff()); err != nil {
			slog.Error("ReleaseSoftBrake failed", "err", err)
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		r.softBrakeActive.Store(false)
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

// ── LiftTakingSkill ───────────────────────────────────────────────────────────

func (r *SlamtecRobot) EnterLift(ctx context.Context, lift agentskill.Lift) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)

		// 1. Get elevator details
		var elevator Elevator
		if err := r.get("/api/multi-floor/map/v1/elevators/"+lift.ID, &elevator); err != nil {
			slog.Error("EnterLift: getElevator failed", "liftId", lift.ID, "err", err)
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		if len(elevator.FrontSchedulingPoses) == 0 {
			err := fmt.Errorf("elevator %s has no front scheduling poses", lift.ID)
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		front := elevator.FrontSchedulingPoses[0]

		// 2. Navigate to front of elevator
		slog.Info("EnterLift: moving to elevator front", "x", front.X, "y", front.Y)
		movePayload := action.NewMoveToActionXYYaw(front.X, front.Y, front.Yaw)
		movePayload.Options.MoveOptions.Mode = defaultMoveMode
		if err := r.submitAction(ctx, movePayload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}

		// 3. Enter elevator
		slog.Info("EnterLift: entering elevator", "elevatorId", elevator.ID)
		enterPayload := action.NewEnterElevatorAction(elevator.ID)
		if err := r.submitAction(ctx, enterPayload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}

		// 4. Rotate 180°
		slog.Info("EnterLift: rotating 180°")
		rotatePayload := action.NewRotateAction(math.Pi)
		if err := r.submitAction(ctx, rotatePayload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}

		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) ExitLift(ctx context.Context, lift agentskill.Lift) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("ExitLift", "liftId", lift.ID)
		payload := action.NewLeaveElevatorAction(lift.ID)
		if err := r.submitAction(ctx, payload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) LocaliseInLift(_ agentskill.Lift, floor agentskill.Floor) bool {
	slog.Info("LocaliseInLift", "building", floor.Building, "level", floor.Level)
	if err := r.put("/api/multi-floor/map/v1/floors/:current", updateFloor{Building: floor.Building, Floor: floor.Level}); err != nil {
		slog.Error("LocaliseInLift: updateFloor failed", "err", err)
		return false
	}
	return true
}

func (r *SlamtecRobot) LiftStatusCh() <-chan agentskill.LiftStatus {
	return nil
}

// ── JackUpDownSkill ───────────────────────────────────────────────────────────

func (r *SlamtecRobot) StartJackUp(ctx context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("StartJackUp: sending Up command")
		if err := r.sendJackCommand(ctx, jackUp, "StartJackUp"); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		slog.Info("StartJackUp: polling for stage=5")
		if err := r.pollJackStatus(ctx, 5, "StartJackUp"); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

func (r *SlamtecRobot) StartJackDown(ctx context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("StartJackDown: sending Down command")
		if err := r.sendJackCommand(ctx, jackDown, "StartJackDown"); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		slog.Info("StartJackDown: polling for stage=2")
		if err := r.pollJackStatus(ctx, 2, "StartJackDown"); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

// ── AdvancedJackUpDownSkill ───────────────────────────────────────────────────

func (r *SlamtecRobot) JackUp(ctx context.Context, tagX, tagY, tagYaw, _, _ float64, _ []int) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("JackUp: docking under tag", "x", tagX, "y", tagY, "yaw", tagYaw)

		payload := action.NewMoveToTagAction(tagX, tagY, tagYaw)
		if err := r.submitAction(ctx, payload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}

		slog.Info("JackUp: docking complete, activating lift")
		result := <-r.StartJackUp(ctx)
		sendResult(ch, result)
	}()
	return ch
}

func (r *SlamtecRobot) JackDown(ctx context.Context) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("JackDown: deactivating lift then backing off")

		result := <-r.StartJackDown(ctx)
		if result.Status != agentskill.Success {
			sendResult(ch, result)
			return
		}

		slog.Info("JackDown: backing off from tag")
		payload := action.NewBackOffFromTagAction()
		if err := r.submitAction(ctx, payload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

// ── MoveToTagSkill ────────────────────────────────────────────────────────────

func (r *SlamtecRobot) MoveToTagWithOptions(ctx context.Context, options map[string]any) <-chan agentskill.Result {
	ch := resultCh()
	go func() {
		defer close(ch)
		slog.Info("MoveToTagWithOptions", "options", options)
		payload := struct {
			ActionName string         `json:"action_name"`
			Options    map[string]any `json:"options"`
		}{
			ActionName: "slamtec.agent.actions.MoveToTagAction",
			Options:    options,
		}
		if err := r.submitAction(ctx, payload); err != nil {
			sendResult(ch, agentskill.Result{Err: err, Status: agentskill.Failed})
			return
		}
		sendResult(ch, agentskill.Result{Status: agentskill.Success})
	}()
	return ch
}

// ── Diagnostic API calls ──────────────────────────────────────────────────────

func (r *SlamtecRobot) summarizeCurrentDock() string {
	var resp SlamtecCurrentDockResponse
	if err := r.get("/api/multi-floor/map/v1/homedocks/:current", &resp); err != nil {
		return "<unavailable: " + err.Error() + ">"
	}
	if !resp.Result {
		return fmt.Sprintf("{bound=false,msg=%s}", resp.Msg)
	}
	d := resp.Data
	return fmt.Sprintf("{bound=true,id=%s,name=%s,building=%s,floor=%s,pose={%.3f,%.3f,%.3f}}",
		d.ID, d.DockName, d.Building, d.Floor, d.Pose.X, d.Pose.Y, d.Pose.Yaw)
}

func (r *SlamtecRobot) summarizeRecentEvents() string {
	var events []SlamtecRobotEvent
	if err := r.get("/api/platform/v1/events", &events); err != nil {
		return "<unavailable>"
	}
	relevant := []string{}
	for _, e := range events {
		if r.isRelevantDockingEvent(e.Type) {
			relevant = append(relevant, fmt.Sprintf("{type=%s,ts=%s}", e.Type, e.Timestamp))
			if len(relevant) >= maxRelevantEvents {
				break
			}
		}
	}
	if len(relevant) == 0 {
		for i, e := range events {
			if i >= maxRelevantEvents {
				break
			}
			relevant = append(relevant, fmt.Sprintf("{type=%s,ts=%s}", e.Type, e.Timestamp))
		}
	}
	return fmt.Sprintf("%v", relevant)
}

func (r *SlamtecRobot) isRelevantDockingEvent(t string) bool {
	switch t {
	case EventMoveToLandingPointFailed, EventSearchDockFailed, EventChargingBaseFailed,
		EventDockIDNotFound, EventPathOccupied, EventRobotBlocked,
		EventCurrentPoseOccupied, EventCliffDetected, EventBumperTriggered,
		EventOnDock, EventOffDock:
		return true
	}
	return false
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func resultCh() chan agentskill.Result {
	return make(chan agentskill.Result, 1)
}

func sendResult(ch chan<- agentskill.Result, r agentskill.Result) {
	select {
	case ch <- r:
	default:
	}
}
