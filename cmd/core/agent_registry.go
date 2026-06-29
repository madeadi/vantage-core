package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

type TaskRepo interface {
	SaveTask(task *Task) error
	GetActiveTasksByAgent(agentID AgentID) []*Task
	ListTasks(agentID AgentID) []*Task
	GetTaskByID(taskID string) (*Task, error)
}

type AgentRegistry struct {
	mu                sync.RWMutex
	tokens            map[AgentID]string
	allowedAgents     []AllowedAgent // pre-shared key → agentID, issued per device at provisioning
	onlineAgents      map[AgentID]*Agent
	streams           map[AgentID]*agentStream
	skills            map[AgentID][]AgentSkill
	grpcAdvertiseAddr string

	poseListener *PoseListener
	stop         func() // cancels the poseListener background goroutine

	taskRepo TaskRepo
}

func NewAgentRegistry(
	allowedAgents []AllowedAgent,
	grpcAdvertiseAddr string,
	taskRepo TaskRepo,
) *AgentRegistry {
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
		taskRepo:          taskRepo,
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

func (r *AgentRegistry) Register(agentID AgentID, skills []AgentSkill) (string, error) {
	slog.Info("Registering Agent", "agentsdk", agentID, "skills", len(skills))
	for _, skill := range skills {
		slog.Info("Agent Skill", "skill_name", skill.Name, "agent_id", agentID)
	}

	token, err := generateRandomHex(32)
	if err != nil {
		slog.Error("Failed to generate token", "err", err)
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens[agentID] = token
	r.skills[agentID] = skills

	return token, nil
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
	if active := r.taskRepo.GetActiveTasksByAgent(task.AgentID); len(active) > 0 {
		return errors.New("agent is busy")
	}
	if err := r.taskRepo.SaveTask(task); err != nil {
		slog.Error("Failed to save task to task repository", "err", err)
		return err
	}

	if err := as.stream.Send(msg); err != nil {
		slog.Error("Failed to send task to agent", "err", err)
		return err
	}

	return nil
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

func (r *AgentRegistry) ListTasks(agentID AgentID) []*Task {
	return r.taskRepo.ListTasks(agentID)
}

func (r *AgentRegistry) Close() {
	r.stop()
}

func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (r *AgentRegistry) OnReconnect(agentID AgentID) {
	activeTasks := r.taskRepo.GetActiveTasksByAgent(agentID)
	if len(activeTasks) == 0 {
		return
	}

	err := r.SendTask(activeTasks[0]) // send the first active task to the agent
	slog.Info("Reconnected agent", "agentID", agentID, "taskID", activeTasks[0].ID)
	if err != nil {
		slog.Error("Failed to send active task to agent", "err", err)
		return
	}
}
