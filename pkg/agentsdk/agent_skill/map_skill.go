package agentskill

import "context"

type MapSkill interface {
	ChangeMap(ctx context.Context, newMap string) <-chan Result
}

type ZoneSkill interface {
	SyncZones()
}
