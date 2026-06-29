package agentskill

import "context"

type LiftStatus int

const (
	LiftEntering LiftStatus = iota
	LiftEntered
	LiftExiting
	LiftExited
	LiftFailed
)

type LiftTakingSkill interface {
	EnterLift(ctx context.Context, lift Lift) <-chan Result
	ExitLift(ctx context.Context, lift Lift) <-chan Result
	LocaliseInLift(lift Lift, floor Floor) bool
	LiftStatusCh() <-chan LiftStatus
}
