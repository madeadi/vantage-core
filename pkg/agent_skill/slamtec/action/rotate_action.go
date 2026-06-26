package action

type RotateAction struct {
	Action
	Options rotateOptions `json:"options"`
}

type rotateOptions struct {
	Angle float64 `json:"angle"`
}

func NewRotateAction(radians float64) RotateAction {
	return RotateAction{
		Action:  Action{ActionName: "slamtec.agent.actions.RotateAction"},
		Options: rotateOptions{Angle: radians},
	}
}
