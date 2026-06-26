package agentskill

import "context"

type TurnSkill interface {
	TurnBy(ctx context.Context, degree float32) <-chan Result
	TurnTo(ctx context.Context, angle float32) <-chan Result
}
