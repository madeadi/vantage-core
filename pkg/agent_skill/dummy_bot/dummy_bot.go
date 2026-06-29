package dummybot

import (
	"math/rand"
	agentskill "vantageos-core/pkg/agent_skill"
)

type DummyBot struct {
}

func New() *DummyBot {
	return &DummyBot{}
}

func (d *DummyBot) GetRobotPose() agentskill.RobotPose {
	return agentskill.RobotPose{
		X:   rand.Float64(),
		Y:   rand.Float64(),
		Yaw: rand.Float64(),
	}
}
