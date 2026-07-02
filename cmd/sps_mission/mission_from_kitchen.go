package main

import (
	"log/slog"

	missionv1 "vantageos-core/proto/mission/v1"
)

type FKDeliveryStatus string

const (
	// ==================== FROM_KITCHEN Flow ====================

	// Phase 0: Initialisation
	FKDeliveryStatusNotStarted        FKDeliveryStatus = "NOT_STARTED"
	FKDeliveryStatusReady             FKDeliveryStatus = "READY"
	FKDeliveryStatusVehicleDispatched FKDeliveryStatus = "VEHICLE_DISPATCHED"

	// Phase 1: Entering Kitchen
	FKDeliveryStatusVehicleAtKitchenEntrance FKDeliveryStatus = "VEHICLE_AT_KITCHEN_ENTRANCE"
	FKDeliveryStatusVehicleEnteringKitchen   FKDeliveryStatus = "VEHICLE_ENTERING_KITCHEN"

	// Phase 2: Kitchen Loading
	FKDeliveryStatusVehicleAtKitchen         FKDeliveryStatus = "VEHICLE_AT_KITCHEN"
	FKDeliveryStatusReadyToLoadAtKitchen     FKDeliveryStatus = "READY_TO_LOAD_AT_KITCHEN"
	FKDeliveryStatusLoadingAtKitchen         FKDeliveryStatus = "LOADING_AT_KITCHEN"
	FKDeliveryStatusLoadingAtKitchenComplete FKDeliveryStatus = "LOADING_AT_KITCHEN_COMPLETE"

	// Phase 3: Exiting Kitchen
	FKDeliveryStatusVehicleToKitchenExit  FKDeliveryStatus = "VEHICLE_TO_KITCHEN_EXIT"
	FKDeliveryStatusVehicleAtKitchenExit  FKDeliveryStatus = "VEHICLE_AT_KITCHEN_EXIT"
	FKDeliveryStatusVehicleExitingKitchen FKDeliveryStatus = "VEHICLE_EXITING_KITCHEN"

	// Phase 4: Transit
	FKDeliveryStatusVehicleTransitingToInstitution FKDeliveryStatus = "VEHICLE_TRANSITING_TO_INSTITUTION"

	// Phase 5: Institution Unloading
	FKDeliveryStatusVehicleAtInstitution           FKDeliveryStatus = "VEHICLE_AT_INSTITUTION"
	FKDeliveryStatusReadyToUnloadAtInstitution     FKDeliveryStatus = "READY_TO_UNLOAD_AT_INSTITUTION"
	FKDeliveryStatusUnloadingAtInstitution         FKDeliveryStatus = "UNLOADING_AT_INSTITUTION"
	FKDeliveryStatusUnloadingAtInstitutionComplete FKDeliveryStatus = "UNLOADING_AT_INSTITUTION_COMPLETE"

	// Phase 6: Loading to Lift
	FKDeliveryStatusLiftAtGroundFloor     FKDeliveryStatus = "LIFT_AT_GROUND_FLOOR"
	FKDeliveryStatusReadyToLoadAtLift     FKDeliveryStatus = "READY_TO_LOAD_AT_LIFT"
	FKDeliveryStatusLoadingToLift         FKDeliveryStatus = "LOADING_TO_LIFT"
	FKDeliveryStatusLoadingToLiftComplete FKDeliveryStatus = "LOADING_TO_LIFT_COMPLETE"

	// Phase 7: Unloading to Destination
	FKDeliveryStatusLiftAtDestination              FKDeliveryStatus = "LIFT_AT_DESTINATION"
	FKDeliveryStatusReadyToUnloadAtDestination     FKDeliveryStatus = "READY_TO_UNLOAD_AT_DESTINATION"
	FKDeliveryStatusUnloadingToDestination         FKDeliveryStatus = "UNLOADING_TO_DESTINATION"
	FKDeliveryStatusUnloadingToDestinationComplete FKDeliveryStatus = "UNLOADING_TO_DESTINATION_COMPLETE"

	// Phase 8: Robot Returning Home
	FKDeliveryStatusRobotInsideLiftToHome FKDeliveryStatus = "ROBOT_INSIDE_LIFT_TO_HOME"
	FKDeliveryStatusReadyToReturnToHome   FKDeliveryStatus = "READY_TO_RETURN_TO_HOME"
	FKDeliveryStatusReturningToHome       FKDeliveryStatus = "RETURNING_TO_HOME"
	FKDeliveryStatusReturnedToHome        FKDeliveryStatus = "RETURNED_TO_HOME"

	// ==================== TO_KITCHEN Flow ====================

	// Phase 1: Getting trolley from top
	FKDeliveryStatusLiftOpeningAtGround FKDeliveryStatus = "LIFT_OPENING_AT_GROUND"
	FKDeliveryStatusRobotEnteringLift   FKDeliveryStatus = "ROBOT_ENTERING_LIFT"
	FKDeliveryStatusLiftGoingToTop      FKDeliveryStatus = "LIFT_GOING_TO_TOP"
	FKDeliveryStatusLoadingToLiftTop    FKDeliveryStatus = "LOADING_TO_LIFT_TOP"

	// Phase 2: Moving to ground
	FKDeliveryStatusLiftGoingToGround       FKDeliveryStatus = "LIFT_GOING_TO_GROUND"
	FKDeliveryStatusUnloadingFromLiftGround FKDeliveryStatus = "UNLOADING_FROM_LIFT_GROUND"
	FKDeliveryStatusUnloadingComplete       FKDeliveryStatus = "UNLOADING_COMPLETE"

	// Phase 3: Loading to vehicle at institution
	FKDeliveryStatusLoadingToVehicle FKDeliveryStatus = "LOADING_TO_VEHICLE"
	FKDeliveryStatusLoadingComplete  FKDeliveryStatus = "LOADING_COMPLETE"

	// Phase 4: Transit to kitchen
	FKDeliveryStatusVehicleToKitchenEntrance FKDeliveryStatus = "VEHICLE_TO_KITCHEN_ENTRANCE"
	FKDeliveryStatusGateOpening              FKDeliveryStatus = "GATE_OPENING"
	FKDeliveryStatusVehicleDockingKitchen    FKDeliveryStatus = "VEHICLE_DOCKING_KITCHEN"

	// Phase 5: Unloading at kitchen
	FKDeliveryStatusUnloadingAtKitchen         FKDeliveryStatus = "UNLOADING_AT_KITCHEN"
	FKDeliveryStatusUnloadingAtKitchenComplete FKDeliveryStatus = "UNLOADING_AT_KITCHEN_COMPLETE"

	// Phase 6: Robot charging
	FKDeliveryStatusRobotCharging FKDeliveryStatus = "ROBOT_CHARGING"

	// Phase 7: Vehicle exiting
	FKDeliveryStatusVehicleGoingHome FKDeliveryStatus = "VEHICLE_GOING_HOME"

	// Terminal
	FKDeliveryStatusDone FKDeliveryStatus = "DONE"

	// Exception States
	FKDeliveryStatusRetrying  FKDeliveryStatus = "RETRYING"
	FKDeliveryStatusFailed    FKDeliveryStatus = "FAILED"
	FKDeliveryStatusCancelled FKDeliveryStatus = "CANCELLED"
)

// FKPhase identifies which step of the FROM_KITCHEN_V2 flow a task
// belongs to. Echoed back verbatim on TaskSchema.mission_context so
// HandleTaskUpdate knows which branch to run next.
type FKPhase string

const (
	FKGotoKitchenEntrance  FKPhase = "FK_GOTO_KITCHEN_ENTRANCE"
	FKOpenGate             FKPhase = "FK_OPEN_GATE"
	FKDockKitchen          FKPhase = "FK_DOCK_KITCHEN"
	FKOpenVehicleDoor      FKPhase = "FK_OPEN_VEHICLE_DOOR"
	FKLoadCargoFromKitchen FKPhase = "FK_LOAD_CARGO_FROM_KITCHEN"
	FKRobotGoKitchenHome   FKPhase = "FK_ROBOT_GO_KITCHEN_HOME"
	FKGotoKitchenExit      FKPhase = "FK_GOTO_KITCHEN_EXIT"
	FKOpenGateExit         FKPhase = "FK_OPEN_GATE_EXIT"
	FKGotoInstitution      FKPhase = "FK_GOTO_INSTITUTION"
	FKOpenVehicleDoorInst  FKPhase = "FK_OPEN_VEHICLE_DOOR_INST"
	FKUnloadAtInstitution  FKPhase = "FK_UNLOAD_AT_INSTITUTION"
	FKOpenLiftGround       FKPhase = "FK_OPEN_LIFT_GROUND"
	FKLoadToLift           FKPhase = "FK_LOAD_TO_LIFT"
	FKOpenLiftTop          FKPhase = "FK_OPEN_LIFT_TOP"
	FKLocaliseLiftTop      FKPhase = "FK_LOCALISE_LIFT_TOP"
	FKUnloadFromLift       FKPhase = "FK_UNLOAD_FROM_LIFT"
	FKOpenLiftGroundReturn FKPhase = "FK_OPEN_LIFT_GROUND_RETURN"
	FKLocaliseLiftGround   FKPhase = "FK_LOCALISE_LIFT_GROUND"
	FKReturnHome           FKPhase = "FK_RETURN_HOME"
)

type MissionFromKitchen struct {
	MissionBase
}

func NewMissionFromKitchen(cfg FromKitchenConfig, dr DeliveryRepository) *MissionFromKitchen {
	return &MissionFromKitchen{
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

func (m *MissionFromKitchen) Type() string {
	return DeliveryTypeFromKitchen
}

// taskFactory wraps the base with FKPhase typing for call-site ergonomics.
func (m *MissionFromKitchen) taskFactory(delivery Delivery, phase FKPhase) *missionv1.CreateTask {
	return m.MissionBase.taskFactory(delivery, string(phase))
}

func (m *MissionFromKitchen) updateDeliveryStatus(delivery Delivery, status FKDeliveryStatus) Delivery {
	return m.MissionBase.updateDeliveryStatus(delivery, string(status))
}

func (m *MissionFromKitchen) Start(delivery Delivery) error {
	slog.Info("starting delivery from kitchen", "delivery_id", delivery.ID)
	delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleDispatched)

	ts := m.taskFactory(delivery, FKGotoKitchenEntrance)
	m.vehicle.GoTo(ts, LocationKitchenEntrance)
	return m.send(ts)
}

func (m *MissionFromKitchen) HandleTaskUpdate(update *missionv1.TaskStatusUpdate) {
	m.handleTaskUpdate(update, string(FKDeliveryStatusFailed), func(delivery Delivery, phase string) {
		m.handleFinishedTask(delivery, FKPhase(phase))
	})
}

func (m *MissionFromKitchen) handleFinishedTask(delivery Delivery, phase FKPhase) {
	switch phase {
	case FKGotoKitchenEntrance:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleAtKitchenEntrance)
		ts := m.taskFactory(delivery, FKOpenGate)
		m.gate.Open(ts)
		m.sendOrLog(ts)

	case FKOpenGate:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleEnteringKitchen)
		ts := m.taskFactory(delivery, FKDockKitchen)
		m.vehicle.Dock(ts, LocationKitchenDock)
		m.sendOrLog(ts)

	case FKDockKitchen:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleAtKitchen)
		ts := m.taskFactory(delivery, FKOpenVehicleDoor)
		m.vehicle.OpenDoor(ts)
		m.sendOrLog(ts)

	case FKOpenVehicleDoor:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLoadingAtKitchen)
		ts := m.taskFactory(delivery, FKLoadCargoFromKitchen)
		m.kRobot.DeliverSingle(ts, LocationKitchenDock, LocationAVHome)
		m.sendOrLog(ts)

	case FKLoadCargoFromKitchen:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLoadingAtKitchenComplete)
		ts := m.taskFactory(delivery, FKRobotGoKitchenHome)
		m.kRobot.GoHome(ts)
		m.sendOrLog(ts)

	case FKRobotGoKitchenHome:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleToKitchenExit)
		ts := m.taskFactory(delivery, FKGotoKitchenExit)
		m.vehicle.GoTo(ts, LocationKitchenExit)
		m.sendOrLog(ts)

	case FKGotoKitchenExit:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleAtKitchenExit)
		ts := m.taskFactory(delivery, FKOpenGateExit)
		m.gate.Open(ts)
		m.sendOrLog(ts)

	case FKOpenGateExit:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleTransitingToInstitution)
		ts := m.taskFactory(delivery, FKGotoInstitution)
		m.vehicle.Dock(ts, LocationInstitutionDock)
		m.sendOrLog(ts)

	case FKGotoInstitution:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusVehicleAtInstitution)
		ts := m.taskFactory(delivery, FKOpenVehicleDoorInst)
		m.vehicle.OpenDoor(ts)
		m.sendOrLog(ts)

	case FKOpenVehicleDoorInst:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusUnloadingAtInstitution)
		ts := m.taskFactory(delivery, FKUnloadAtInstitution)
		m.iRobot.DeliverSingle(ts, LocationInstitutionDock, LocationInstitutionGroundLift)
		m.sendOrLog(ts)

	case FKUnloadAtInstitution:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLiftAtGroundFloor)
		ts := m.taskFactory(delivery, FKOpenLiftGround)
		m.lift.OpenAt(ts, "GROUND_FLOOR")
		m.sendOrLog(ts)

	case FKOpenLiftGround:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLoadingToLift)
		ts := m.taskFactory(delivery, FKLoadToLift)
		m.iRobot.DeliverSingle(ts, LocationInstitutionGroundLift, LocationInstitutionTopLift)
		m.sendOrLog(ts)

	case FKLoadToLift:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusLiftAtDestination)
		ts := m.taskFactory(delivery, FKOpenLiftTop)
		m.lift.OpenAt(ts, "INSTITUTION_FLOOR")
		m.sendOrLog(ts)

	case FKOpenLiftTop:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusUnloadingToDestination)
		ts := m.taskFactory(delivery, FKLocaliseLiftTop)
		m.iRobot.Localise(ts, LocationInstitutionTopLift)
		m.sendOrLog(ts)

	case FKLocaliseLiftTop:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusUnloadingToDestination)
		ts := m.taskFactory(delivery, FKUnloadFromLift)
		m.iRobot.DeliverSingle(ts, LocationInstitutionTopLift, LocationInstitutionDock)
		m.sendOrLog(ts)

	case FKUnloadFromLift:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusRobotInsideLiftToHome)
		ts := m.taskFactory(delivery, FKOpenLiftGroundReturn)
		m.lift.OpenAt(ts, "GROUND_FLOOR")
		m.sendOrLog(ts)

	case FKOpenLiftGroundReturn:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusReturningToHome)
		ts := m.taskFactory(delivery, FKLocaliseLiftGround)
		m.iRobot.Localise(ts, LocationInstitutionGroundLift)
		m.sendOrLog(ts)

	case FKLocaliseLiftGround:
		delivery = m.updateDeliveryStatus(delivery, FKDeliveryStatusReturningToHome)
		ts := m.taskFactory(delivery, FKReturnHome)
		m.iRobot.GoHome(ts)
		m.sendOrLog(ts)

	case FKReturnHome:
		m.updateDeliveryStatus(delivery, FKDeliveryStatusDone)
		slog.Info("delivery completed successfully", "delivery_id", delivery.ID)

	default:
		slog.Warn("unknown task context", "context", phase, "delivery_id", delivery.ID)
	}
}
