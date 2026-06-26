package agentskill

import "context"

type GoToOption struct {
	Forward   bool
	Bubble    float64
	Behaviour string
	MoveMode  int
}

type GoToSkill interface {
	GoToNamedTarget(ctx context.Context, namedTarget string, options GoToOption) <-chan Result
	GoToXY(ctx context.Context, x, y, yaw float64) <-chan Result
	StopGo(ctx context.Context) <-chan Result
}
