package agentskill

import "context"

type TtsSkill interface {
	PlayTts(ctx context.Context, text string) <-chan Result
	StopTts(ctx context.Context) <-chan Result
}
