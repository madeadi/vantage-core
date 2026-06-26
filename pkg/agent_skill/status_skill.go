package agentskill

type RobotStatus int

const (
	RobotStatusInit RobotStatus = iota
	RobotStatusIdle
	RobotStatusAutonomous
	RobotStatusDocking
	RobotStatusDocked
	RobotStatusCharging
	RobotStatusRemoteControl
	RobotStatusTeleop
	RobotStatusSoftBrake
	RobotStatusEstop
	RobotStatusFatal
	RobotStatusMaintenance
)

type StatusSkill interface {
	GetBatteryPercentage() int
	GetBatteryVoltage() float64
	GetEstimatedRuntime() int
	GetEstimatedChargetime() int
	IsBatteryCharging() bool
	GetSignalStrength() int
	IsMainTaskExecuting() bool
}

type RobotStatusSkill interface {
	GetRobotStatuses() []map[string]bool
	IsStatusActive(key string) bool
}
