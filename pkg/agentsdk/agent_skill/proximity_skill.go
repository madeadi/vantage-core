package agentskill

import "context"

type ProximityDetectionModeSkill interface {
	EnableProximityDetection(ctx context.Context) <-chan Result
	DisableProximityDetection(ctx context.Context) <-chan Result
	ProximityMessages() <-chan []byte
}

type SurroundingAwarenessModeSkill interface {
	EnableSurroundingAwareness(ctx context.Context, gridSize, voxelSize float64) <-chan Result
	DisableSurroundingAwareness(ctx context.Context) <-chan Result
	OccupancyMessages() <-chan []byte
}
