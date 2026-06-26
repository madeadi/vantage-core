package agentskill

import "context"

type MoveToTagSkill interface {
	MoveToTagWithOptions(ctx context.Context, options map[string]any) <-chan Result
}
