package main

import (
	"errors"
	"log/slog"
	"sync"

	missionv1 "vantageos-core/proto/mission/v1"
)

// MissionRegistry stores the state of missions: progress, status, and tasks
type MissionRegistry struct {
	mu            sync.RWMutex
	missions      map[MissionID]*Mission
	missionTasks  map[MissionID][]Task
	taskToMission map[string]MissionID
	streams       map[MissionID]missionv1.MissionService_StreamMissionServer
}

func NewMissionRegistry() *MissionRegistry {
	return &MissionRegistry{
		missions:      make(map[MissionID]*Mission),
		missionTasks:  make(map[MissionID][]Task),
		taskToMission: make(map[string]MissionID),
		streams:       make(map[MissionID]missionv1.MissionService_StreamMissionServer),
	}
}

type StartMission struct {
	ID        MissionID
	HandlerID MissionHandlerID
	Name      string
}

// StartOrResume starts or resumes a mission, sending MissionReady to the stream.
// @todo: need to reserve the resources.
func (mr *MissionRegistry) StartOrResume(
	sm *StartMission,
	stream missionv1.MissionService_StreamMissionServer,
) {
	mr.mu.Lock()

	mission, resumed := mr.missions[sm.ID]
	if !resumed {
		mission = &Mission{
			ID:        sm.ID,
			HandlerID: sm.HandlerID,
			Name:      sm.Name,
			Status:    MissionStatusPending,
		}
		mr.missions[sm.ID] = mission
	}
	mr.streams[sm.ID] = stream

	// build task snapshots for resume
	var snapshots []*missionv1.TaskSnapshot
	if resumed {
		for _, task := range mr.missionTasks[sm.ID] {
			snapshots = append(snapshots, &missionv1.TaskSnapshot{
				TaskId:  task.ID,
				AgentId: string(task.AgentID),
				Status:  toMissionTaskStatus(task.Status),
			})
		}
	}

	mr.mu.Unlock()

	stream.Send(&missionv1.MissionServerMessage{
		Payload: &missionv1.MissionServerMessage_MissionReady{
			MissionReady: &missionv1.MissionReady{
				Resumed: resumed,
				Tasks:   snapshots,
			},
		},
	})
}

// End terminates a mission session and notifies the client.
func (mr *MissionRegistry) End(id MissionID, reason string) {
	mr.mu.Lock()
	stream, ok := mr.streams[id]
	delete(mr.streams, id)
	mr.mu.Unlock()

	if !ok {
		return
	}

	stream.Send(&missionv1.MissionServerMessage{
		Payload: &missionv1.MissionServerMessage_MissionEnded{
			MissionEnded: &missionv1.MissionEnded{Reason: reason},
		},
	})
}

func (mr *MissionRegistry) AddTask(msId MissionID, task Task) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	m, ok := mr.missions[msId]
	if !ok {
		return errors.New("mission does not exist")
	}
	if !m.Active() {
		return errors.New("mission is not active")
	}

	m.Status = MissionStatusRunning
	mr.missionTasks[msId] = append(mr.missionTasks[msId], task)
	mr.taskToMission[task.ID] = msId

	return nil
}

func (mr *MissionRegistry) UpdateExecutionStatus(task Task) {
	if task.Transient() {
		slog.Info("Transient status, ignoring.")
		return
	}

	mr.mu.Lock()

	ms := mr.missions[task.MissionID]
	if ms == nil || !ms.Active() {
		mr.mu.Unlock()
		slog.Error("mission is not active", "missionID", task.MissionID)
		return
	}

	stream, ok := mr.streams[task.MissionID]
	if !ok {
		mr.mu.Unlock()
		slog.Error("no stream for mission", "missionID", task.MissionID)
		return
	}

	for i := range mr.missionTasks[task.MissionID] {
		if mr.missionTasks[task.MissionID][i].ID == task.ID {
			mr.missionTasks[task.MissionID][i].Status = task.Status
			mr.missionTasks[task.MissionID][i].Result = task.Result
			break
		}
	}

	mr.mu.Unlock()

	stream.Send(&missionv1.MissionServerMessage{
		Payload: &missionv1.MissionServerMessage_TaskStatusUpdate{
			TaskStatusUpdate: &missionv1.TaskStatusUpdate{
				TaskId:  task.ID,
				Status:  toMissionTaskStatus(task.Status),
				AgentId: string(task.AgentID),
				Output:  task.Result,
			},
		},
	})
}

func toMissionTaskStatus(agentStatus TaskStatus) missionv1.MissionTaskStatus {
	switch agentStatus {
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
