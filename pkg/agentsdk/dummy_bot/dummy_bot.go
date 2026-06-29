package dummybot

import (
	"context"
	"math/rand"
	"time"
	agentskill2 "vantageos-core/pkg/agentsdk/agent_skill"
)

type DummyBot struct {
	skillDuration time.Duration
	skillChan     chan agentskill2.Result
}

func New() *DummyBot {
	skillChan := make(chan agentskill2.Result, 1)
	return &DummyBot{
		skillDuration: 3 * time.Second,
		skillChan:     skillChan,
	}
}

func (d *DummyBot) GetRobotPose() agentskill2.RobotPose {
	return agentskill2.RobotPose{
		X:   rand.Float64(),
		Y:   rand.Float64(),
		Yaw: rand.Float64(),
	}
}

func (d *DummyBot) GoToNamedTarget(ctx context.Context, namedTarget string, options agentskill2.GoToOption) <-chan agentskill2.Result {
	ch := make(chan agentskill2.Result, 1)
	go func() {
		select {
		case <-time.After(d.skillDuration):
			ch <- agentskill2.Result{Status: agentskill2.Success}
		case <-ctx.Done():
			ch <- agentskill2.Result{Status: agentskill2.Cancelled}
		}
	}()
	return ch
}

func (d *DummyBot) GoToXY(ctx context.Context, x, y, yaw float64) <-chan agentskill2.Result {
	ch := make(chan agentskill2.Result, 1)
	go func() {
		select {
		case <-time.After(d.skillDuration):
			ch <- agentskill2.Result{Status: agentskill2.Success}
		case <-ctx.Done():
			ch <- agentskill2.Result{Status: agentskill2.Cancelled}
		}
	}()
	return ch
}

func (d *DummyBot) StopGo(ctx context.Context) <-chan agentskill2.Result {
	ch := make(chan agentskill2.Result, 1)
	ch <- agentskill2.Result{Status: agentskill2.Success}
	return ch
}

func (d *DummyBot) GoChargeNearby(ctx context.Context) <-chan agentskill2.Result {
	ch := make(chan agentskill2.Result, 1)
	ch <- agentskill2.Result{Status: agentskill2.Success}
	return ch
}
