package agentskill

import "context"

type SoftBrakeSkill interface {
	ActivateSoftBrake(ctx context.Context) <-chan Result
	ReleaseSoftBrake(ctx context.Context) <-chan Result
}

type EmergencyBrakeSkill interface {
	BrakeCh() <-chan bool
	IsBrakeOn() bool
}
