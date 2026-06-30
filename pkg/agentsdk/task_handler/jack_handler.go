package task_handler

import (
	"context"
	"encoding/json"
	"fmt"
	agentskill "vantageos-core/pkg/agentsdk/agent_skill"
	agentv1 "vantageos-core/proto/agent/v1"
)

type JackHandler struct {
	jackSkill agentskill.JackUpDownSkill
}

func NewJackHandler(jackSkill agentskill.JackUpDownSkill) *JackHandler {
	return &JackHandler{jackSkill: jackSkill}
}

func (j JackHandler) GetTaskType() string {
	return "JACK"
}

func (j JackHandler) Execute(ctx context.Context, task *agentv1.Task) (result []byte, err error) {
	var p JackPayload

	if err := json.Unmarshal(task.GetPayload(), &p); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	var ch <-chan agentskill.Result

	switch p.Direction {
	case Up:
		ch = j.jackSkill.StartJackUp(ctx)
	case Down:
		ch = j.jackSkill.StartJackDown(ctx)
	}

	select {
	case r := <-ch:
		return nil, r.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type JackPayload struct {
	Direction Direction `json:"direction"`
}

type Direction string

const (
	Up   Direction = "up"
	Down Direction = "down"
)
