package dummybot

import (
	"context"
	"math/rand"
	"time"
	agentskill "vantageos-core/pkg/agent_skill"
)

type DummyBot struct {
	skillDuration time.Duration
	skillChan     chan agentskill.Result
}

func New() *DummyBot {
	skillChan := make(chan agentskill.Result, 1)
	return &DummyBot{
		skillDuration: 3 * time.Second,
		skillChan:     skillChan,
	}
}

func (d *DummyBot) GetRobotPose() agentskill.RobotPose {
	return agentskill.RobotPose{
		X:   rand.Float64(),
		Y:   rand.Float64(),
		Yaw: rand.Float64(),
	}
}

func (d *DummyBot) GoToNamedTarget(ctx context.Context, namedTarget string, options agentskill.GoToOption) <-chan agentskill.Result {
	ch := make(chan agentskill.Result, 1)
	go func() {
		select {
		case <-time.After(d.skillDuration):
			ch <- agentskill.Result{Status: agentskill.Success}
		case <-ctx.Done():
			ch <- agentskill.Result{Status: agentskill.Cancelled}
		}
	}()
	return ch
}

func (d *DummyBot) GoToXY(ctx context.Context, x, y, yaw float64) <-chan agentskill.Result {
	ch := make(chan agentskill.Result, 1)
	go func() {
		select {
		case <-time.After(d.skillDuration):
			ch <- agentskill.Result{Status: agentskill.Success}
		case <-ctx.Done():
			ch <- agentskill.Result{Status: agentskill.Cancelled}
		}
	}()
	return ch
}

func (d *DummyBot) StopGo(ctx context.Context) <-chan agentskill.Result {
	ch := make(chan agentskill.Result, 1)
	ch <- agentskill.Result{Status: agentskill.Success}
	return ch
}
