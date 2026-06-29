package agentskill

import "context"

type TeleopSkill interface {
	StartTeleop(ctx context.Context) <-chan Result
	StopTeleop(ctx context.Context) <-chan Result
	AbortTeleop(ctx context.Context) <-chan Result
}
