package task_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"vantageos-core/pkg/agentsdk/agent_skill"
	agentv1 "vantageos-core/proto/agent/v1"
)

type GoHomeHandler struct {
	chargingSkill agentskill.ChargingSkill
}

func NewGoHomeHandler(chargingSkill agentskill.ChargingSkill) *GoHomeHandler {
	return &GoHomeHandler{chargingSkill: chargingSkill}
}

func (g GoHomeHandler) GetTaskType() string {
	return "GO_HOME"
}

func (g GoHomeHandler) Execute(ctx context.Context, task *agentv1.Task) (result []byte, err error) {
	var p Payload
	if err := json.Unmarshal(task.GetPayload(), &p); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	ch := g.chargingSkill.GoChargeNearby(ctx)
	select {
	case r := <-ch:
		return nil, r.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
