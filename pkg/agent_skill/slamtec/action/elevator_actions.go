package action

type elevatorOptions struct {
	ElevatorID           string               `json:"elevator_id"`
	EnterElevatorOptions enterElevatorOptions `json:"enter_elevator_options"`
}

type enterElevatorOptions struct {
	ElevatorStoppingYaw string `json:"elevator_stopping_yaw"`
	UseConservativeMode bool   `json:"use_conservative_mode"`
}

func defaultElevatorOptions(elevatorID string) elevatorOptions {
	return elevatorOptions{
		ElevatorID: elevatorID,
		EnterElevatorOptions: enterElevatorOptions{
			ElevatorStoppingYaw: "face_to_front_door",
			UseConservativeMode: true,
		},
	}
}

type EnterElevatorAction struct {
	Action
	Options elevatorOptions `json:"options"`
}

func NewEnterElevatorAction(elevatorID string) EnterElevatorAction {
	return EnterElevatorAction{
		Action:  Action{ActionName: "slamtec.agent.actions.EnterElevatorAction"},
		Options: defaultElevatorOptions(elevatorID),
	}
}

type LeaveElevatorAction struct {
	Action
	Options elevatorOptions `json:"options"`
}

func NewLeaveElevatorAction(elevatorID string) LeaveElevatorAction {
	return LeaveElevatorAction{
		Action:  Action{ActionName: "slamtec.agent.actions.LeaveElevatorAction"},
		Options: defaultElevatorOptions(elevatorID),
	}
}
