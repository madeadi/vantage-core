package main

import agentskill "vantageos-core/pkg/agent_skill"

type Robot interface {
	agentskill.RobotPoseSkill
	agentskill.GoToSkill
}
