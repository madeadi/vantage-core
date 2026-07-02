package service

import (
	"sync"
	"vantageos-core/internal/core/config"
	"vantageos-core/internal/core/model"
	missionv1 "vantageos-core/proto/mission/v1"
)

// MissionRegistry stores the state of missions. Missions are external services that
// is responsible to orchestrate tasks.
type MissionRegistry struct {
	mu         sync.RWMutex
	missions   map[string]*model.Mission
	streams    map[string]missionv1.MissionService_StreamMissionServer
	authTokens map[string]string // store the auth token for each mission

	missionTasks  map[string][]model.Task
	taskToMission map[string]string

	// mission that is allowed to join
	alloweds    []config.MissionConfig
	authService AuthService
}

func NewMissionRegistry(configs []config.MissionConfig) *MissionRegistry {
	authSvc := NewAuthService()
	for _, cfg := range configs {
		authSvc.AddRegToken(string(cfg.ID), cfg.Key)
	}

	return &MissionRegistry{
		missions:      make(map[string]*model.Mission),
		missionTasks:  make(map[string][]model.Task),
		taskToMission: make(map[string]string),
		streams:       make(map[string]missionv1.MissionService_StreamMissionServer),
		alloweds:      configs,
		authTokens:    make(map[string]string),
		authService:   authSvc,
	}
}

// Register a mission along with the requirements
func (mr *MissionRegistry) Register(missionID string) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	if _, exists := mr.missions[missionID]; exists {
		return nil
	}

	name := ""
	for _, cfg := range mr.alloweds {
		if cfg.ID == missionID {
			name = cfg.Name
			break
		}
	}

	mr.missions[missionID] = &model.Mission{
		ID:     missionID,
		Name:   name,
		Status: model.MissionStatusPending,
	}
	return nil
}

// MissionInfo is a read-only view of a registered mission's status.
type MissionInfo struct {
	ID     string // missionID
	Name   string
	Online bool
	Status model.MissionStatus
}

// ListMissions returns every configured mission along with its online/running status.
func (mr *MissionRegistry) ListMissions() []MissionInfo {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	infos := make([]MissionInfo, 0, len(mr.alloweds))
	for _, cfg := range mr.alloweds {
		_, online := mr.streams[cfg.ID]
		status := model.MissionStatusPending
		if m, ok := mr.missions[cfg.ID]; ok {
			status = m.Status
		}
		infos = append(infos, MissionInfo{
			ID:     cfg.ID,
			Name:   cfg.Name,
			Online: online,
			Status: status,
		})
	}
	return infos
}

func (mr *MissionRegistry) ExchangeRegToken(regToken string) (string, string, error) {
	return mr.authService.ExchangeRegToken(regToken)
}

func (mr *MissionRegistry) Authenticate(missionID model.MissionID, authToken string) bool {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return mr.authService.Authenticate(string(missionID), authToken)
}

// AttachStream marks a mission online and stores its live gRPC stream.
func (mr *MissionRegistry) AttachStream(missionID string, s missionv1.MissionService_StreamMissionServer) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.streams[missionID] = s
}

// DetachStream marks a mission offline and removes its stream.
func (mr *MissionRegistry) DetachStream(missionID string) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	delete(mr.streams, missionID)
}

func (mr *MissionRegistry) GetStream(missionID string) missionv1.MissionService_StreamMissionServer {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return mr.streams[missionID]
}
