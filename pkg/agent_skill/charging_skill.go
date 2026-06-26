package agentskill

import "context"

type ChargingSkill interface {
	GoChargeNearby(ctx context.Context) <-chan Result
}
