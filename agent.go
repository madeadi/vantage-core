package main

type AgentID string

type Agent struct {
	ID           AgentID
	Name         string
	Skills       []AgentSkill
	EventSources []EventSource
}

type AgentSkill struct {
	Name    string
	Payload AgentPayload
}

type AgentPayload struct {
	Name string
}

type EventSource struct {
	Name string
}
