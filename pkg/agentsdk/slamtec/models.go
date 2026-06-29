package slamtec

import (
	"vantageos-core/pkg/agentsdk/slamtec/action"
)

// ── POJO / shared types ───────────────────────────────────────────────────────

type Pose struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Yaw   float64 `json:"yaw"`
	Pitch float64 `json:"pitch,omitempty"`
	Roll  float64 `json:"roll,omitempty"`
	Z     float64 `json:"z,omitempty"`
}

type PoiMetadata struct {
	Name string `json:"display_name"`
}

// SlamtecPoiSingleFloor is a POI returned by /api/core/artifact/v1/pois.
type SlamtecPoiSingleFloor struct {
	ID       string      `json:"id"`
	Pose     action.Pose `json:"pose"`
	Metadata PoiMetadata `json:"metadata"`
}

type SlamtecFloor struct {
	Building       string `json:"building"`
	Floor          string `json:"floor"`
	IsDefaultFloor bool   `json:"isDefaultFloor"`
}

type SlamtecDockInfo struct {
	ID       string `json:"id"`
	DockName string `json:"dock_name"`
	Floor    string `json:"floor"`
	Building string `json:"building"`
	Pose     Pose   `json:"pose"`
}

type SlamtecRobotEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
}

const (
	EventDeviceError              = "DEVICE_ERROR"
	EventPathOccupied             = "PATH_OCCUPIED"
	EventRobotBlocked             = "ROBOT_BLOCKED"
	EventOnDock                   = "ON_DOCK"
	EventOffDock                  = "OFF_DOCK"
	EventMoveToLandingPointFailed = "MOVE_TO_LANDING_POINT_FAILED"
	EventSearchDockFailed         = "SEARCH_DOCK_FAILED"
	EventChargingBaseFailed       = "CHARGING_BASE_FAILED"
	EventDockIDNotFound           = "DOCK_ID_NOT_FOUND"
	EventBumperTriggered          = "BUMPER_TRIGGERED"
	EventCurrentPoseOccupied      = "CURRENT_POSE_OCCUPIED"
	EventCliffDetected            = "CLIFF_DETECTED"
)

// ── Response types ────────────────────────────────────────────────────────────

type PowerStatus struct {
	BatteryPercentage int           `json:"batteryPercentage"`
	IsCharging        bool          `json:"isCharging"`
	DockingStatus     DockingStatus `json:"dockingStatus"`
}

type DockingStatus string

const (
	DockingStatusDocked   DockingStatus = "on_dock"
	DockingStatusUndocked DockingStatus = "not_on_dock"
)

type NetworkStatus struct {
	Quality  int    `json:"quality"`
	SSID     string `json:"ssid"`
	IP       string `json:"ip"`
	SignalDB int    `json:"signal_db"`
}

type ActionInfo struct {
	ActionID   int         `json:"action_id"`
	ActionName string      `json:"action_name"`
	Stage      string      `json:"stage"`
	State      ActionState `json:"state"`
}

type ActionState struct {
	Status int    `json:"status"`
	Result int    `json:"result"`
	Reason string `json:"reason"`
}

const (
	actionStatusNew  = 0
	actionStatusDone = 4

	actionResultSuccess = 0
	actionResultFailed  = -1
)

type RobotHealth struct {
	BaseError                  []BaseError `json:"baseError"`
	HasDepthCameraDisconnected bool        `json:"hasDepthCameraDisconnected"`
	HasError                   bool        `json:"hasError"`
	HasFatal                   bool        `json:"hasFatal"`
	HasLidarDisconnected       bool        `json:"hasLidarDisconnected"`
	HasSdpDisconnected         bool        `json:"hasSdpDisconnected"`
	HasSystemEmergencyStop     bool        `json:"hasSystemEmergencyStop"`
	HasWarning                 bool        `json:"hasWarning"`
}

type BaseError struct {
	Component              int    `json:"component"`
	ComponentErrorCode     int    `json:"componentErrorCode"`
	ComponentErrorDeviceID int    `json:"componentErrorDeviceId"`
	ComponentErrorType     int    `json:"componentErrorType"`
	ErrorCode              int    `json:"errorCode"`
	ID                     int    `json:"id"`
	Level                  int    `json:"level"`
	Message                string `json:"message"`
}

type JackStatus struct {
	ActualPos int `json:"actual_pos"`
	Alarm     int `json:"alarm"`
	DrvStatus int `json:"drv_status"`
	Stage     int `json:"stage"`
}

type Elevator struct {
	ID                   string `json:"elevator_id"`
	DoorType             string `json:"door_type"`
	FrontSchedulingPoses []Pose `json:"front_scheduling_poses"`
	Optional             bool   `json:"optional"`
}

type SlamtecCurrentDockResponse struct {
	Result bool            `json:"result"`
	Msg    string          `json:"msg"`
	Data   SlamtecDockInfo `json:"data"`
}

// ── Request helper types ──────────────────────────────────────────────────────

type systemParameter struct {
	Param string `json:"param"`
	Value string `json:"value"`
}

func paramEmergencyStopOn() systemParameter {
	return systemParameter{Param: "base.emergency_stop", Value: "on"}
}

func paramEmergencyStopOff() systemParameter {
	return systemParameter{Param: "base.emergency_stop", Value: "off"}
}

type jackStatusValue string

const (
	jackUp   jackStatusValue = "Up"
	jackDown jackStatusValue = "Down"
)

type updateFloor struct {
	Building string `json:"building"`
	Floor    string `json:"floor"`
	Pose     *Pose  `json:"pose,omitempty"`
}
