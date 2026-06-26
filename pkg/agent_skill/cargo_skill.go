package agentskill

import "context"

type CargoSkill interface {
	OpenCargoDoor(ctx context.Context, id string) <-chan Result
	CloseCargoDoor(ctx context.Context, id string) <-chan Result
	IsCargoDoorClosed(id string) bool
	ListCargoIDs() []string
	IsCargoOccupied(id string) bool
}
