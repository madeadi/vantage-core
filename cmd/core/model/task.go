package model

import (
	"time"
	missionv1 "vantageos-core/proto/mission/v1"
)

type Task struct {
	ID      string
	AgentID AgentID
	Type    string // e.g. "GO_TO", "GO_HOME"
	Payload []byte
	Status  TaskStatus
	Result  []byte

	// Mission related fields
	MissionID        string // e.g. "patrol, from_kitchen", etc.
	MissionContextID string // e.g. delivery ID
	MissionContext   []byte

	ReceivedAt time.Time
	StartAt    time.Time
	ToExpireAt time.Time
}

type TaskStatus int

const (
	TaskStatusUnspecified TaskStatus = iota // 0
	TaskStatusDraft                         // 1
	TaskStatusStarting                      // 2
	TaskStatusCannotStart                   // 3
	TaskStatusStarted                       // 4
	TaskStatusExpiring                      // 5
	TaskStatusExpired                       // 6
	TaskStatusAborting                      // 7
	TaskStatusAborted                       // 8
	TaskStatusFailed                        // 9
	TaskStatusFinishing                     // 10
	TaskStatusFinished                      // 11
)

// Transient returns true if the task is in a transient state (not yet getting confirmation from the agentsdk).
func (t *Task) Transient() bool {
	switch t.Status {
	case TaskStatusUnspecified, TaskStatusDraft, TaskStatusStarting, TaskStatusExpiring, TaskStatusAborting, TaskStatusFinishing:
		return true
	default:
		return false
	}
}

func (t *Task) Final() bool {
	return t.Status == TaskStatusAborted || t.Status == TaskStatusFailed || t.Status == TaskStatusFinished
}

func (t *Task) CopyMetadata(tn *Task) {
	tn.ReceivedAt = t.ReceivedAt
	tn.StartAt = t.StartAt
	tn.ToExpireAt = t.ToExpireAt
}

func (t *Task) MissionTaskStatus() missionv1.MissionTaskStatus {
	switch t.Status {
	case TaskStatusStarted:
		return missionv1.MissionTaskStatus_MISSION_TASK_STATUS_IN_PROGRESS
	case TaskStatusFinished:
		return missionv1.MissionTaskStatus_MISSION_TASK_STATUS_COMPLETED
	case TaskStatusFailed, TaskStatusCannotStart, TaskStatusAborted, TaskStatusExpired:
		return missionv1.MissionTaskStatus_MISSION_TASK_STATUS_FAILED
	default:
		return missionv1.MissionTaskStatus_MISSION_TASK_STATUS_UNSPECIFIED
	}
}
