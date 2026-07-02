package service

import (
	"log/slog"
	"sync"
	"vantageos-core/internal/core/model"
	"vantageos-core/pkg/agentsdk"
	agentv1 "vantageos-core/proto/agent/v1"
)

type AllowedAgent struct {
	AgentID model.AgentID
	Name    string
	Key     string
}

type agentStream struct {
	stream agentv1.AgentService_StreamTasksServer
	mu     sync.Mutex
}

type AgentRegistry struct {
	mu                sync.RWMutex
	authService       AuthService
	allowedAgents     []AllowedAgent // pre-shared key → agentID, issued per device at provisioning
	onlineAgents      map[model.AgentID]*model.Agent
	streams           map[model.AgentID]*agentStream
	skills            map[model.AgentID][]model.AgentSkill
	cameras           map[model.AgentID][]agentsdk.CameraConfig
	grpcAdvertiseAddr string
}

func NewAgentRegistry(
	allowedAgents []AllowedAgent,
	grpcAdvertiseAddr string,
) *AgentRegistry {
	slog.Info("NewAgentRegistry")

	authSvc := NewAuthService()
	for _, allowedAgent := range allowedAgents {
		authSvc.AddRegToken(string(allowedAgent.AgentID), allowedAgent.Key)
	}

	return &AgentRegistry{
		onlineAgents:      make(map[model.AgentID]*model.Agent),
		streams:           make(map[model.AgentID]*agentStream),
		skills:            make(map[model.AgentID][]model.AgentSkill),
		cameras:           make(map[model.AgentID][]agentsdk.CameraConfig),
		allowedAgents:     allowedAgents,
		grpcAdvertiseAddr: grpcAdvertiseAddr,
		authService:       authSvc,
	}
}

func (r *AgentRegistry) GrpcAdvertiseAddr() string {
	return r.grpcAdvertiseAddr
}

func (r *AgentRegistry) ExchangeRegToken(regToken string) (string, string, error) {
	return r.authService.ExchangeRegToken(regToken)
}

func (r *AgentRegistry) Register(agentID model.AgentID, skills []model.AgentSkill, cameras []agentsdk.CameraConfig) {
	slog.Info("Registering Agent", "agentsdk", agentID, "skills", len(skills))
	for _, skill := range skills {
		slog.Info("Agent Skill", "skill_name", skill.Name, "agent_id", agentID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[agentID] = skills
	r.cameras[agentID] = cameras
}

func (r *AgentRegistry) NameFor(agentID model.AgentID) string {
	for _, a := range r.allowedAgents {
		if a.AgentID == agentID {
			return a.Name
		}
	}
	return ""
}

// AttachStream marks an agentsdk online and stores its live gRPC stream.
func (r *AgentRegistry) AttachStream(agentID model.AgentID, s agentv1.AgentService_StreamTasksServer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onlineAgents[agentID] = &model.Agent{ID: agentID, Name: r.NameFor(agentID)}
	r.streams[agentID] = &agentStream{stream: s}
}

// DetachStream marks an agentsdk offline and removes its stream.
func (r *AgentRegistry) DetachStream(agentID model.AgentID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.onlineAgents, agentID)
	delete(r.streams, agentID)
}

// SendToAgent serializes and sends msg on the agent's live gRPC stream, if any.
// online is false when the agent has no active stream.
func (r *AgentRegistry) SendToAgent(agentID model.AgentID, msg *agentv1.ServerMessage) (online bool, err error) {
	r.mu.RLock()
	as, ok := r.streams[agentID]
	r.mu.RUnlock()
	if !ok {
		return false, nil
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	return true, as.stream.Send(msg)
}

func (r *AgentRegistry) OnlineAgents() map[model.AgentID]*model.Agent {
	return r.onlineAgents
}

func (r *AgentRegistry) GetCameras(agentID model.AgentID) []agentsdk.CameraConfig {
	return r.cameras[agentID]
}

func (r *AgentRegistry) AllowedAgents() []AllowedAgent {
	return r.allowedAgents
}

func (r *AgentRegistry) AuthService() *AuthService {
	return &r.authService
}
