package task_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"vantageos-core/pkg/agentsdk/agent_skill"

	agentv1 "vantageos-core/proto/agent/v1"

	"github.com/google/jsonschema-go/jsonschema"
)

type GotoPayload struct {
	LocationName string  `json:"locationName"`
	LayoutID     string  `json:"layoutId,omitempty"`
	Yaw          float64 `json:"yaw,omitempty"`
}

type GotoHandler struct {
	goToSkill agentskill.GoToSkill
}

func NewGotoHandler(skill agentskill.GoToSkill) *GotoHandler {
	return &GotoHandler{goToSkill: skill}
}

func (g GotoHandler) GetTaskType() string {
	return "GOTO"
}

func (g GotoHandler) Execute(ctx context.Context, task *agentv1.Task) ([]byte, error) {
	var p GotoPayload
	if err := json.Unmarshal(task.GetPayload(), &p); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	if p.LocationName == "" {
		return nil, fmt.Errorf("locationName is required")
	}
	ch := g.goToSkill.GoToNamedTarget(ctx, p.LocationName, agentskill.GoToOption{Forward: true})
	select {
	case r := <-ch:
		return nil, r.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (g GotoHandler) GetPayloadSchema() string {
	if schema, err := jsonschema.For[GotoPayload](nil); err != nil {
		slog.Error("failed to generate schema", "err", err)
		return ""
	} else {
		out, _ := json.Marshal(schema)
		return string(out)
	}
}
