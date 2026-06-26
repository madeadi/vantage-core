package agentskill

import "context"

type MoveSkill interface {
	MoveForward(ctx context.Context) <-chan Result
	MoveBackward(ctx context.Context) <-chan Result
	TurnLeft(ctx context.Context) <-chan Result
	TurnRight(ctx context.Context) <-chan Result
	StopMoving(ctx context.Context) <-chan Result
	Move(ctx context.Context, linearVel, angularVel float64) <-chan Result
}

type MovementSkill interface {
	StartMoving(ctx context.Context, x, y float32) <-chan Result
	StopMoving(ctx context.Context) <-chan Result
	TurnBy(ctx context.Context, degree int) <-chan Result
	Move(ctx context.Context, linearXSpeed, angularZSpeed float64) <-chan Result
	Turn(ctx context.Context, angularZSpeed float64) <-chan Result
}
