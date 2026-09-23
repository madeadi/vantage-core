package service

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
	"vantageos-core/cmd/core/model"
	"vantageos-core/pkg/agentsdk"
	agentv1 "vantageos-core/proto/agent/v1"
)

// mqttPresenceStaleAfter bounds how long an MQTT agent is still considered
// online after its last liveness signal (an online/registered event, or --
// as a bonus signal when it happens to be flowing -- an ingested telemetry
// message) before OnlineAgents stops reporting it, even without an
// explicit offline event ever arriving.
//
// The LWT (see pkg/agentsdk.NewAgent's SetBinaryWill) and retained
// online/offline events (spec Step 16) are the primary presence signal and
// transition immediately; this is only the last-resort safety net for the
// case where neither ever arrives, e.g. a network partition between core
// and the broker that outlasts core's own reconnect. There is no periodic
// re-affirmation of "online" once connected (retained events are set once,
// not republished on a timer), and telemetry may not be flowing at all for
// a task-only agent -- so this must be generous, not a tight health-check
// interval, or a perfectly healthy, quiet connection would age out on its
// own.
const mqttPresenceStaleAfter = 2 * time.Minute

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

	// mqttLastSeen tracks presence for agents connected over MQTT (spec
	// Step 16) -- a separate signal from onlineAgents/streams, which is
	// gRPC-stream-specific. OnlineAgents unions both: an agent counts as
	// online if it has a live gRPC stream OR a not-yet-stale MQTT signal.
	mqttLastSeen map[model.AgentID]time.Time
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
		mqttLastSeen:      make(map[model.AgentID]time.Time),
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

// MarkMQTTOnline records agentID as alive via MQTT: an explicit
// online/registered event, or -- as a bonus signal, when it happens to be
// flowing -- an ingested telemetry message. See mqttPresenceStaleAfter's
// doc comment for how this and MarkMQTTOffline interact with OnlineAgents.
func (r *AgentRegistry) MarkMQTTOnline(agentID model.AgentID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mqttLastSeen[agentID] = time.Now()
}

// MarkMQTTOffline records agentID as explicitly gone: an offline event
// (graceful disconnect or the broker delivering the LWT). Immediate, unlike
// the staleness timeout -- an explicit offline event is a stronger signal
// than "we simply have not heard from it in a while."
func (r *AgentRegistry) MarkMQTTOffline(agentID model.AgentID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mqttLastSeen, agentID)
}

// OnlineAgents returns every agent currently considered online: gRPC
// agents with a live stream, unioned with MQTT agents whose last liveness
// signal is within mqttPresenceStaleAfter. Returns a fresh copy, safe for
// the caller to read without further locking.
func (r *AgentRegistry) OnlineAgents() map[model.AgentID]*model.Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[model.AgentID]*model.Agent, len(r.onlineAgents)+len(r.mqttLastSeen))
	for id, a := range r.onlineAgents {
		cp := *a
		out[id] = &cp
	}
	now := time.Now()
	for id, lastSeen := range r.mqttLastSeen {
		if now.Sub(lastSeen) >= mqttPresenceStaleAfter {
			continue
		}
		if _, already := out[id]; already {
			continue
		}
		out[id] = &model.Agent{ID: id, Name: r.NameFor(id)}
	}
	return out
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
