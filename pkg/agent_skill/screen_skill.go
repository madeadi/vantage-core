package agentskill

import "context"

type ScreenStatus int

const (
	ScreenOn ScreenStatus = iota
	ScreenOff
)

type ScreenSkill interface {
	DisplayMediaOnScreen(ctx context.Context, media string) <-chan Result
	DisplayTextOnScreen(ctx context.Context, text string) <-chan Result
	ResetScreen(ctx context.Context) <-chan Result
	GetScreenStatus() ScreenStatus
	WhatsPlayingOnScreen() string
}
