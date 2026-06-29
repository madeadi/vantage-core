package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
	agentv1 "vantageos-core/proto/agent/v1"
)

type AllowedAgent struct {
	AgentID AgentID
	Name    string
	Key     string
}

type agentStream struct {
	stream agentv1.AgentService_StreamTasksServer
	mu     sync.Mutex
}

type AgentRegistry struct {
	mu                sync.RWMutex
	tokens            map[AgentID]string
	onlineAgents      map[AgentID]*Agent
	streams           map[AgentID]*agentStream
	skills            map[AgentID][]AgentSkill
	allowedAgents     []AllowedAgent // pre-shared key → agentID, issued per device at provisioning
	grpcAdvertiseAddr string

	poseListener *PoseListener
	stop         func() // cancels the poseListener background goroutine
}

func NewAgentRegistry(allowedAgents []AllowedAgent, grpcAdvertiseAddr string) *AgentRegistry {
	slog.Info("NewAgentRegistry")
	for _, allowedAgent := range allowedAgents {
		slog.Info("Agent", "agentID", allowedAgent.AgentID, "name", allowedAgent.Name)
	}

	keepPoseHistory := 1 * time.Hour
	poseListener := NewPoseListener(keepPoseHistory)
	poseCtx, cancelPose := context.WithCancel(context.Background())
	go poseListener.Run(poseCtx)

	return &AgentRegistry{
		onlineAgents:      make(map[AgentID]*Agent),
		tokens:            make(map[AgentID]string),
		streams:           make(map[AgentID]*agentStream),
		skills:            make(map[AgentID][]AgentSkill),
		allowedAgents:     allowedAgents,
		grpcAdvertiseAddr: grpcAdvertiseAddr,
		poseListener:      poseListener,
		stop:              cancelPose,
	}
}

func (r *AgentRegistry) AddAllowedAgent(agent AllowedAgent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, allowedAgent := range r.allowedAgents {
		if allowedAgent.Key == agent.Key || allowedAgent.AgentID == agent.AgentID {
			return errors.New("AgentID or Key is not unique")
		}
	}
	r.allowedAgents = append(r.allowedAgents, agent)
	return nil
}

func (r *AgentRegistry) Authenticate(agentID AgentID, token string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tokens[agentID]
	if !ok {
		return false
	}
	return t == token
}

func (r *AgentRegistry) Register(agent *Agent, token string, skills []AgentSkill) {
	slog.Info("Registering Agent", "agentsdk", agent.ID, "skills", len(skills))
	for _, skill := range skills {
		slog.Info("Agent Skill", "skill_name", skill.Name, "agentsdk", agent.ID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens[agent.ID] = token
	r.skills[agent.ID] = skills
}

func (r *AgentRegistry) Unregister(agentID AgentID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.onlineAgents, agentID)
}

func (r *AgentRegistry) AddSkill(agentID AgentID, skill AgentSkill) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.isOnlineLocked(agentID) {
		return errors.New("Agent not found")
	}
	if !r.verifySkillPayload(skill) {
		return errors.New("Invalid skill payload")
	}
	for i, s := range r.skills[agentID] {
		if s.Name == skill.Name {
			r.skills[agentID][i] = skill
			return nil
		}
	}
	r.skills[agentID] = append(r.skills[agentID], skill)
	return nil
}

func (r *AgentRegistry) IsOnline(agentID AgentID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isOnlineLocked(agentID)
}

// isOnlineLocked checks online status; caller must hold at least r.mu.RLock.
func (r *AgentRegistry) isOnlineLocked(agentID AgentID) bool {
	_, ok := r.onlineAgents[agentID]
	return ok
}

func (r *AgentRegistry) nameFor(agentID AgentID) string {
	for _, a := range r.allowedAgents {
		if a.AgentID == agentID {
			return a.Name
		}
	}
	return ""
}

// attachStream marks an agentsdk online and stores its live gRPC stream.
func (r *AgentRegistry) attachStream(agentID AgentID, s agentv1.AgentService_StreamTasksServer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onlineAgents[agentID] = &Agent{ID: agentID, Name: r.nameFor(agentID)}
	r.streams[agentID] = &agentStream{stream: s}
}

// detachStream marks an agentsdk offline and removes its stream.
func (r *AgentRegistry) detachStream(agentID AgentID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.onlineAgents, agentID)
	delete(r.streams, agentID)
}

// SendTask pushes a task down the agentsdk's live gRPC stream.
func (r *AgentRegistry) SendTask(task *Task) error {
	r.mu.RLock()
	as, ok := r.streams[task.AgentID]
	r.mu.RUnlock()
	if !ok {
		return errors.New("no active stream for agentsdk")
	}
	msg := &agentv1.ServerMessage{
		Payload: &agentv1.ServerMessage_Task{Task: &agentv1.Task{
			Id:      task.ID,
			Type:    task.Type,
			Payload: task.Payload,
		}},
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.stream.Send(msg)
}

func (r *AgentRegistry) verifySkillPayload(skill AgentSkill) bool {
	return true
}

func (r *AgentRegistry) GetSkill(agentID AgentID, name string) (AgentSkill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, skill := range r.skills[agentID] {
		if skill.Name == name {
			return skill, true
		}
	}
	return AgentSkill{}, false
}

func (r *AgentRegistry) OnPoseUpdate(agentID AgentID, event *agentv1.PoseTelemetryEvent) {
	r.poseListener.OnPoseUpdate(agentID, event)
}

func (r *AgentRegistry) Close() {
	r.stop()
}
