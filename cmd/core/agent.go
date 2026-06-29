package main

import "encoding/json"

type AgentID string

type Agent struct {
	ID   AgentID `json:"id"`
	Name string  `json:"name"`
}

type AgentSkill struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

type EventSource struct {
	Name string `json:"name"`
}
