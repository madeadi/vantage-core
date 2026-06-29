package main

import "time"

type LayoutPose struct {
	AgentID   AgentID
	LayoutID  string
	X         float64
	Y         float64
	Yaw       float64
	Timestamp time.Time
}
