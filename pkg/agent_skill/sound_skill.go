package agentskill

import "context"

type SoundStatus int

const (
	SoundOn SoundStatus = iota
	SoundOff
)

type SoundSkill interface {
	PlayMediaSound(ctx context.Context, media string) <-chan Result
	PlayTextSound(ctx context.Context, text string) <-chan Result
	ResetSound(ctx context.Context) <-chan Result
	GetSoundStatus() SoundStatus
	WhatsPlayingOnSound() string
}
