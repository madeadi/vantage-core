package main

import (
	agentskill2 "vantageos-core/pkg/agentsdk/agent_skill"
)

type Robot interface {
	agentskill2.RobotPoseSkill
	agentskill2.GoToSkill
	agentskill2.ChargingSkill
	agentskill2.JackUpDownSkill
}
