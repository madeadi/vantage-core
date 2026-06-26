package agentskill

import "context"

type LocaliseSkill interface {
	AutoLocalise(ctx context.Context, layoutID, layoutName, buildingName string) <-chan Result
	ManualLocalise(ctx context.Context, layoutID, layoutName, buildingName string, pose RobotPose) <-chan Result
}

type RobotPoseSkill interface {
	GetRobotPose() RobotPose
}
