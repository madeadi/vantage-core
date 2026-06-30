package agentskill

import "context"

type JackUpDownSkill interface {
	StartJackUp(ctx context.Context) <-chan Result
	StartJackDown(ctx context.Context) <-chan Result
}
