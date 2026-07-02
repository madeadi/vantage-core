package main

import (
	"log/slog"

	missionv1 "vantageos-core/proto/mission/v1"
)

// TKPhase identifies which step of the TO_KITCHEN_V2 flow a task
// belongs to. Echoed back verbatim on mission_context so HandleTaskUpdate
// knows which branch to run next.
type TKPhase string

const (
	TKOpenLiftGround         TKPhase = "TK_OPEN_LIFT_GROUND"
	TKEnterLift              TKPhase = "TK_ENTER_LIFT"
	TKOpenLiftTop            TKPhase = "TK_OPEN_LIFT_TOP"
	TKLocaliseLiftTop        TKPhase = "TK_LOCALISE_LIFT_TOP"
	TKLoadToLiftTop          TKPhase = "TK_LOAD_TO_LIFT_TOP"
	TKOpenLiftGround2        TKPhase = "TK_OPEN_LIFT_GROUND_2"
	TKLocaliseLiftGround     TKPhase = "TK_LOCALISE_LIFT_GROUND"
	TKUnloadFromLift         TKPhase = "TK_UNLOAD_FROM_LIFT"
	TKGotoInstitution        TKPhase = "TK_GOTO_INSTITUTION"
	TKOpenVehicleDoor        TKPhase = "TK_OPEN_VEHICLE_DOOR"
	TKLoadToVehicle          TKPhase = "TK_LOAD_TO_VEHICLE"
	TKGotoKitchenEntrance    TKPhase = "TK_GOTO_KITCHEN_ENTRANCE"
	TKOpenGate               TKPhase = "TK_OPEN_GATE"
	TKDockKitchen            TKPhase = "TK_DOCK_KITCHEN"
	TKOpenVehicleDoorKitchen TKPhase = "TK_OPEN_VEHICLE_DOOR_KITCHEN"
	TKUnloadAtKitchen        TKPhase = "TK_UNLOAD_AT_KITCHEN"
	TKRobotGoHome            TKPhase = "TK_ROBOT_GO_HOME"
	TKGotoKitchenExit        TKPhase = "TK_GOTO_KITCHEN_EXIT"
	TKOpenGateExit           TKPhase = "TK_OPEN_GATE_EXIT"
	TKVehicleGoHome          TKPhase = "TK_VEHICLE_GO_HOME"
)

type MissionToKitchen struct {
	MissionBase
}

func NewMissionToKitchen(cfg ToKitchenConfig, dr DeliveryRepository) *MissionToKitchen {
	return &MissionToKitchen{
		MissionBase: MissionBase{
			vehicle: VehicleAgent{AgentID: cfg.VehicleID},
			gate:    GateAgent{AgentID: cfg.GateID},
			lift:    LiftAgent{AgentID: cfg.LiftID},
			kRobot:  RobotAgent{AgentID: cfg.KitchenRobotID},
			iRobot:  RobotAgent{AgentID: cfg.InstitutionRobotID},
			dr:      dr,
		},
	}
}

func (m *MissionToKitchen) Type() string {
	return DeliveryTypeToKitchen
}

func (m *MissionToKitchen) taskFactory(delivery Delivery, phase TKPhase) *missionv1.CreateTask {
	return m.MissionBase.taskFactory(delivery, string(phase))
}

func (m *MissionToKitchen) updateDeliveryStatus(delivery Delivery, status FKDeliveryStatus) Delivery {
	return m.MissionBase.updateDeliveryStatus(delivery, string(status))
}

func (m *MissionToKitchen) Start(delivery Delivery) error {
	slog.Info("starting delivery to kitchen", "delivery_id", delivery.ID)
	delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLiftOpeningAtGround)

	ts := m.taskFactory(delivery, TKOpenLiftGround)
	m.lift.OpenAt(ts, "GROUND_FLOOR")
	return m.send(ts)
}

func (m *MissionToKitchen) HandleTaskUpdate(update *missionv1.TaskStatusUpdate) {
	m.handleTaskUpdate(update, string(FKDeliveryStatusFailed), func(delivery Delivery, phase string) {
		m.handleFinishedTask(delivery, TKPhase(phase))
	})
}

func (m *MissionToKitchen) handleFinishedTask(delivery Delivery, phase TKPhase) {
	switch phase {
	case TKOpenLiftGround:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusRobotEnteringLift)
		ts := m.taskFactory(delivery, TKEnterLift)
		m.iRobot.GoTo(ts, LocationInstitutionGroundLift)
		m.sendOrLog(ts)

	case TKEnterLift:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLiftGoingToTop)
		ts := m.taskFactory(delivery, TKOpenLiftTop)
		m.lift.OpenAt(ts, "INSTITUTION_FLOOR")
		m.sendOrLog(ts)

	case TKOpenLiftTop:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLoadingToLiftTop)
		ts := m.taskFactory(delivery, TKLocaliseLiftTop)
		m.iRobot.Localise(ts, LocationInstitutionTopLift)
		m.sendOrLog(ts)

	case TKLocaliseLiftTop:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLoadingToLiftTop)
		ts := m.taskFactory(delivery, TKLoadToLiftTop)
		m.iRobot.DeliverSingle(ts, LocationInstitutionDock, LocationInstitutionTopLift)
		m.sendOrLog(ts)

	case TKLoadToLiftTop:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLiftGoingToGround)
		ts := m.taskFactory(delivery, TKOpenLiftGround2)
		m.lift.OpenAt(ts, "GROUND_FLOOR")
		m.sendOrLog(ts)

	case TKOpenLiftGround2:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLoadingToLiftTop)
		ts := m.taskFactory(delivery, TKLocaliseLiftGround)
		m.iRobot.Localise(ts, LocationInstitutionGroundLift)
		m.sendOrLog(ts)

	case TKLocaliseLiftGround:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusUnloadingFromLiftGround)
		ts := m.taskFactory(delivery, TKUnloadFromLift)
		m.iRobot.DeliverSingle(ts, LocationInstitutionTopLift, LocationInstitutionGroundLift)
		m.sendOrLog(ts)

	case TKUnloadFromLift:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleDispatched)
		ts := m.taskFactory(delivery, TKGotoInstitution)
		m.vehicle.GoTo(ts, LocationInstitutionDock)
		m.sendOrLog(ts)

	case TKGotoInstitution:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleAtInstitution)
		ts := m.taskFactory(delivery, TKOpenVehicleDoor)
		m.vehicle.OpenDoor(ts)
		m.sendOrLog(ts)

	case TKOpenVehicleDoor:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLoadingToVehicle)
		ts := m.taskFactory(delivery, TKLoadToVehicle)
		m.iRobot.DeliverSingle(ts, LocationInstitutionGroundLift, LocationInstitutionDock)
		m.sendOrLog(ts)

	case TKLoadToVehicle:
		// Fire-and-forget: send institution robot home without a phase,
		// so its completion doesn't trigger a state-machine transition.
		// Java sets missionType=null; Go omits the phase key instead.
		robotHomeTs := &missionv1.CreateTask{
			MissionContext: &missionv1.MissionContext{
				Id: delivery.ID,
			},
		}
		m.iRobot.GoHome(robotHomeTs)
		m.sendOrLog(robotHomeTs)

		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleToKitchenEntrance)
		ts := m.taskFactory(delivery, TKGotoKitchenEntrance)
		m.vehicle.GoTo(ts, LocationKitchenEntrance)
		m.sendOrLog(ts)

	case TKGotoKitchenEntrance:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusGateOpening)
		ts := m.taskFactory(delivery, TKOpenGate)
		m.gate.Open(ts)
		m.sendOrLog(ts)

	case TKOpenGate:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleDockingKitchen)
		ts := m.taskFactory(delivery, TKDockKitchen)
		m.vehicle.Dock(ts, LocationKitchenDock)
		m.sendOrLog(ts)

	case TKDockKitchen:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleAtKitchen)
		ts := m.taskFactory(delivery, TKOpenVehicleDoorKitchen)
		m.vehicle.OpenDoor(ts)
		m.sendOrLog(ts)

	case TKOpenVehicleDoorKitchen:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusUnloadingAtKitchen)
		ts := m.taskFactory(delivery, TKUnloadAtKitchen)
		m.kRobot.DeliverSingle(ts, LocationAVHome, LocationKitchenDock)
		m.sendOrLog(ts)

	case TKUnloadAtKitchen:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusRobotCharging)
		ts := m.taskFactory(delivery, TKRobotGoHome)
		m.kRobot.GoHome(ts)
		m.sendOrLog(ts)

	case TKRobotGoHome:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleToKitchenExit)
		ts := m.taskFactory(delivery, TKGotoKitchenExit)
		m.vehicle.GoTo(ts, LocationKitchenExit)
		m.sendOrLog(ts)

	case TKGotoKitchenExit:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusGateOpening)
		ts := m.taskFactory(delivery, TKOpenGateExit)
		m.gate.Open(ts)
		m.sendOrLog(ts)

	case TKOpenGateExit:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleGoingHome)
		ts := m.taskFactory(delivery, TKVehicleGoHome)
		m.vehicle.GoHome(ts)
		m.sendOrLog(ts)

	case TKVehicleGoHome:
		m.updateDeliveryStatus(delivery, FKDeliveryStatusDone)
		slog.Info("delivery to kitchen completed successfully", "delivery_id", delivery.ID)

	default:
		slog.Warn("unknown task context", "context", phase, "delivery_id", delivery.ID)
	}
}
