package main

import (
	"encoding/json"
	"log/slog"

	missionv1 "vantageos-core/proto/mission/v1"
)

// Task types. Reuse existing Go conventions where an agent-side executor
// already exists (GOTO/GO_HOME from pkg/agentsdk/task_handler); the rest
// don't have an executor yet, but keep Java's naming so a future Go
// handler can adopt the same wire shape.
const (
	TaskTypeGoto           = "GOTO"
	TaskTypeGoDock         = "GO_DOCK"
	TaskTypeGoHome         = "GO_HOME"
	TaskTypeOpenGate       = "OPEN_GATE"
	TaskTypeOpenLift       = "OPEN_LIFT"
	TaskTypeAVTailgate     = "AV_TAILGATE"
	TaskTypeDeliverSingle  = "DELIVER_SINGLE"
	TaskTypeLocaliseManual = "LOCALISE_MANUAL"
)

// Location names for the FROM_KITCHEN_V2 flow. No Waypoints/NodeRepository
// system exists on the Go side, so these are plain strings rather than an
// enum resolved against a layout.
const (
	LocationKitchenEntrance       = "KITCHEN_ENTRANCE"
	LocationKitchenExit           = "KITCHEN_EXIT"
	LocationKitchenDock           = "KITCHEN_DOCK"
	LocationInstitutionDock       = "INSTITUTION_DOCK"
	LocationAVHome                = "AV_HOME"
	LocationInstitutionGroundLift = "INSTITUTION_GROUND_LIFT"
	LocationInstitutionTopLift    = "INSTITUTION_TOP_LIFT"
)

type GotoPayload struct {
	LocationName string `json:"locationName"`
}

type TailgatePayload struct {
	Open bool `json:"open"`
}

type OpenLiftPayload struct {
	Floor string `json:"floor"`
}

type DeliverSinglePayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type LocalisePayload struct {
	Location string `json:"location"`
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("failed to marshal task payload", "err", err)
		return nil
	}
	return b
}

// VehicleAgent builds tasks targeting the autonomous delivery vehicle.
type VehicleAgent struct{ AgentID string }

func (v VehicleAgent) requirement() *missionv1.TaskRequirements {
	return &missionv1.TaskRequirements{AgentId: v.AgentID}
}

func (v VehicleAgent) GoTo(t *missionv1.CreateTask, location string) {
	t.Type = TaskTypeGoto
	t.Requirement = v.requirement()
	t.Payload = mustJSON(GotoPayload{LocationName: location})
}

func (v VehicleAgent) Dock(t *missionv1.CreateTask, location string) {
	t.Type = TaskTypeGoDock
	t.Requirement = v.requirement()
	t.Payload = mustJSON(GotoPayload{LocationName: location})
}

func (v VehicleAgent) OpenDoor(t *missionv1.CreateTask) {
	t.Type = TaskTypeAVTailgate
	t.Requirement = v.requirement()
	t.Payload = mustJSON(TailgatePayload{Open: true})
}

func (v VehicleAgent) CloseDoor(t *missionv1.CreateTask) {
	t.Type = TaskTypeAVTailgate
	t.Requirement = v.requirement()
	t.Payload = mustJSON(TailgatePayload{Open: false})
}

func (v VehicleAgent) GoHome(t *missionv1.CreateTask) {
	t.Type = TaskTypeGoHome
	t.Requirement = v.requirement()
}

// GateAgent builds tasks targeting a physical gate.
type GateAgent struct{ AgentID string }

func (g GateAgent) Open(t *missionv1.CreateTask) {
	t.Type = TaskTypeOpenGate
	t.Requirement = &missionv1.TaskRequirements{AgentId: g.AgentID}
}

// LiftAgent builds tasks targeting the cargo lift.
type LiftAgent struct{ AgentID string }

func (l LiftAgent) OpenAt(t *missionv1.CreateTask, floor string) {
	t.Type = TaskTypeOpenLift
	t.Requirement = &missionv1.TaskRequirements{AgentId: l.AgentID}
	t.Payload = mustJSON(OpenLiftPayload{Floor: floor})
}

// RobotAgent builds tasks targeting a mobile robot (kitchen or institution side).
//
// Simplification: Java moves trolleys through 4 slots at a time via
// NodeRepository/TrolleyRepository/Layout/Building lookups (DELIVER_MULTIPLE).
// None of that infra exists on the Go side, so these are collapsed to a
// single from/to move with a minimal payload; revisit once that infra exists.
type RobotAgent struct{ AgentID string }

func (r RobotAgent) requirement() *missionv1.TaskRequirements {
	return &missionv1.TaskRequirements{AgentId: r.AgentID}
}

func (r RobotAgent) GoTo(t *missionv1.CreateTask, location string) {
	t.Type = TaskTypeGoto
	t.Requirement = r.requirement()
	t.Payload = mustJSON(GotoPayload{LocationName: location})
}

func (r RobotAgent) DeliverSingle(t *missionv1.CreateTask, from, to string) {
	t.Type = TaskTypeDeliverSingle
	t.Requirement = r.requirement()
	t.Payload = mustJSON(DeliverSinglePayload{From: from, To: to})
}

func (r RobotAgent) Localise(t *missionv1.CreateTask, location string) {
	t.Type = TaskTypeLocaliseManual
	t.Requirement = r.requirement()
	t.Payload = mustJSON(LocalisePayload{Location: location})
}

func (r RobotAgent) GoHome(t *missionv1.CreateTask) {
	t.Type = TaskTypeGoHome
	t.Requirement = r.requirement()
}
