package main

import "encoding/json"

type AgentID string

type Agent struct {
	ID   AgentID `json:"id"`
	Name string  `json:"name"`

	Cameras      []Camera
	Skills       []AgentSkill
	EventSources []EventSource
}

type AgentSkill struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

type EventSource struct {
	Name string `json:"name"`
}

type Camera struct {
	ID   string
	Name string
	Type CameraType
	Url  string
}

type CameraType string

const (
	CameraTypeMJpg CameraType = "mjpg"
	CameraTypeRtsp CameraType = "rtsp"
	CameraTypeHls  CameraType = "hls"
)
