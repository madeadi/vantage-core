package main

import (
	"errors"
	"log/slog"
)

type AllowedAgent struct {
	AgentID AgentID
	Name    string
	Key     string
}

type AgentRegistry struct {
	tokens        map[AgentID]string
	onlineAgents  map[AgentID]*Agent // the online agents
	skills        map[AgentID][]AgentSkill
	allowedAgents []AllowedAgent // pre-shared key → agentID, issued per device at provisioning
	mqttConfig    MQTTConfig
}

func NewAgentRegistry(mqttConfig MQTTConfig, allowedAgents []AllowedAgent) *AgentRegistry {
	slog.Info("NewAgentRegistry")
	for _, allowedAgent := range allowedAgents {
		slog.Info("Agent", "agentID", allowedAgent.AgentID, "name", allowedAgent.Name)
	}

	return &AgentRegistry{
		onlineAgents:  make(map[AgentID]*Agent),
		tokens:        make(map[AgentID]string),
		skills:        make(map[AgentID][]AgentSkill),
		allowedAgents: allowedAgents,
		mqttConfig:    mqttConfig,
	}
}

func (r *AgentRegistry) AddAllowedAgent(agent AllowedAgent) error {
	// ensure that the key and ID are unique
	for _, allowedAgent := range r.allowedAgents {
		if allowedAgent.Key == agent.Key || allowedAgent.AgentID == agent.AgentID {
			return errors.New("AgentID or Key is not unique")
		}
	}

	r.allowedAgents = append(r.allowedAgents, agent)
	return nil
}

func (r *AgentRegistry) Authenticate(agentID AgentID, token string) bool {
	t, ok := r.tokens[agentID]
	if !ok {
		return false
	}
	return t == token
}

func (r *AgentRegistry) Register(agent *Agent, skills []AgentSkill) {
	r.onlineAgents[agent.ID] = agent
	r.skills[agent.ID] = skills
}

func (r *AgentRegistry) Unregister(agentID AgentID) {
	delete(r.onlineAgents, agentID)
}

func (r *AgentRegistry) AddSkill(agentID AgentID, skill AgentSkill) error {
	// if agent is not online, return an error
	if !r.isOnline(agentID) {
		return errors.New("Agent not found")
	}

	// if skill payload is invalid, return an error
	if !r.verifySkillPayload(skill) {
		return errors.New("Invalid skill payload")
	}

	// overwrite the skill if it already exists
	for i, s := range r.skills[agentID] {
		if s.Name == skill.Name {
			r.skills[agentID][i] = skill
			return nil
		}
	}

	// otherwise, append the skill to the list
	r.skills[agentID] = append(r.skills[agentID], skill)
	return nil
}

func (r *AgentRegistry) isOnline(agentID AgentID) bool {
	_, ok := r.onlineAgents[agentID]
	return ok
}

func (r *AgentRegistry) verifySkillPayload(skill AgentSkill) bool {
	if skill.Payload.Name == "" {
		return false
	}
	return true
}

func (r *AgentRegistry) GetSkill(agentID AgentID, name string) (AgentSkill, bool) {
	for _, skill := range r.skills[agentID] {
		if skill.Name == name {
			return skill, true
		}
	}
	return AgentSkill{}, false
}
