package action

type BackOffFromTagAction struct {
	Action
	Options struct{} `json:"options"`
}

func NewBackOffFromTagAction() BackOffFromTagAction {
	return BackOffFromTagAction{
		Action: Action{ActionName: "slamtec.agentsdk.actions.BackOffFromTagAction"},
	}
}
