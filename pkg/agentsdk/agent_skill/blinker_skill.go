package agentskill

import "context"

type BlinkerStatus int

const (
	BlinkerOn BlinkerStatus = iota
	BlinkerOff
)

type BlinkerSkill interface {
	TurnOnBlinker(ctx context.Context) <-chan Result
	PlayPattern(ctx context.Context, pattern string) <-chan Result
	GetBlinkerStatus() BlinkerStatus
	ResetBlinker(ctx context.Context) <-chan Result
	WhatsPlayingOnBlinker() string
}
