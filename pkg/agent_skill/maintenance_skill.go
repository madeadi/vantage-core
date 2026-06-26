package agentskill

import "context"

type MaintenanceModeSkill interface {
	EnableMaintenance(ctx context.Context) <-chan Result
	DisableMaintenance(ctx context.Context) <-chan Result
}
