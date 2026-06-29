package agentskill

type Initialize interface {
	InitRobot()
	SetDefaultLayoutID(layoutID string)
	GetDefaultLayoutID() string
}
