package model

type MissionID string
type MissionHandlerID string

type Mission struct {
	ID     string
	Name   string
	Tasks  []Task
	Status MissionStatus
}

type MissionStatus string

const (
	MissionStatusPending   MissionStatus = "pending"
	MissionStatusAccepted  MissionStatus = "accepted"
	MissionStatusRejected  MissionStatus = "rejected"
	MissionStatusRunning   MissionStatus = "running"
	MissionStatusCompleted MissionStatus = "completed"
	MissionStatusFailed    MissionStatus = "failed"
)

func (m *Mission) Active() bool {
	return m.Status == MissionStatusRunning || m.Status == MissionStatusPending
}
