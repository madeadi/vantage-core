package service

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"vantageos-core/cmd/core/model"
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
	mu          sync.RWMutex
	authService AuthService
	// allowedAgents maps a pre-shared key → agentID, issued per device at
	// provisioning. It is sourced from the "agents" PocketBase collection and
	// swapped wholesale by SetAllowedAgents when that collection changes, so it
	// is held in an atomic pointer rather than guarded by mu.
	allowedAgents     atomic.Pointer[[]AllowedAgent]
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

	r := &AgentRegistry{
		onlineAgents:      make(map[model.AgentID]*model.Agent),
		streams:           make(map[model.AgentID]*agentStream),
		skills:            make(map[model.AgentID][]model.AgentSkill),
		cameras:           make(map[model.AgentID][]agentsdk.CameraConfig),
		grpcAdvertiseAddr: grpcAdvertiseAddr,
		authService:       NewAuthService(),
	}
	r.SetAllowedAgents(allowedAgents)
	return r
}

// SetAllowedAgents atomically replaces the set of agents allowed to register
// and rebuilds the registration-token table from their keys. Safe to call at
// any time; used to apply live edits to the "agents" config collection.
func (r *AgentRegistry) SetAllowedAgents(allowedAgents []AllowedAgent) {
	next := append([]AllowedAgent(nil), allowedAgents...)
	regTokens := make(map[string]string, len(next))
	for _, a := range next {
		regTokens[a.Key] = string(a.AgentID)
	}
	r.authService.ResetRegTokens(regTokens)
	r.allowedAgents.Store(&next)
}

func (r *AgentRegistry) GrpcAdvertiseAddr() string {
	return r.grpcAdvertiseAddr
}

func (r *AgentRegistry) ExchangeRegToken(regToken string) (string, string, error) {
	return r.authService.ExchangeRegToken(regToken)
}

// IssueMQTTCredentials mints a fresh broker password for agentID (spec Step
// 15). See AuthService.IssueMQTTCredentials.
func (r *AgentRegistry) IssueMQTTCredentials(agentID model.AgentID) (string, error) {
	return r.authService.IssueMQTTCredentials(string(agentID))
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
	for _, a := range *r.allowedAgents.Load() {
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

// SetSkills updates the skills reported by an agentsdk without touching its cameras.
func (r *AgentRegistry) SetSkills(agentID model.AgentID, skills []model.AgentSkill) {
	slog.Info("Setting Agent Skills", "agent_id", agentID, "skills", len(skills))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[agentID] = skills
}

func (r *AgentRegistry) SkillsFor(agentID model.AgentID) []model.AgentSkill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.skills[agentID]
}

func (r *AgentRegistry) AllowedAgents() []AllowedAgent {
	return *r.allowedAgents.Load()
}

func (r *AgentRegistry) AuthService() *AuthService {
	return &r.authService
}
