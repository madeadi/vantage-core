package main

type AgentID string

type Agent struct {
	ID   AgentID `json:"id"`
	Name string  `json:"name"`
}

type AgentSkill struct {
	Name    string       `json:"name"`
	Payload AgentPayload `json:"payload"`
}

type AgentPayload struct {
	Name string `json:"name"`
}

type EventSource struct {
	Name string `json:"name"`
}
