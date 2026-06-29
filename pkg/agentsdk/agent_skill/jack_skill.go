package agentskill

import "context"

type JackUpDownSkill interface {
	StartJackUp(ctx context.Context) <-chan Result
	StartJackDown(ctx context.Context) <-chan Result
}

type AdvancedJackUpDownSkill interface {
	JackUpDownSkill
	JackUp(ctx context.Context, tagX, tagY, tagYaw, relativeX, relativeY float64, tagIDs []int) <-chan Result
	JackDown(ctx context.Context) <-chan Result
}
