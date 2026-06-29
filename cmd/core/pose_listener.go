package main

import (
	"context"
	"sync"
	"time"
	agentv1 "vantageos-core/proto/agent/v1"
)

// PoseListener listens to pose telemetry events from agents.
// It stores the historical pose data for each agentsdk for replay and analysis.
type PoseListener struct {
	mu      sync.Mutex
	latest  map[AgentID]LayoutPose
	history map[AgentID][]LayoutPose

	keepFor time.Duration
}

func NewPoseListener(keepFor time.Duration) *PoseListener {
	return &PoseListener{
		latest:  make(map[AgentID]LayoutPose),
		history: make(map[AgentID][]LayoutPose),
		keepFor: keepFor,
	}
}

func (p *PoseListener) Run(ctx context.Context) {
	ticker := time.NewTicker(p.keepFor)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.cleanHistory()
		}
	}
}

func (p *PoseListener) OnPoseUpdate(agentID AgentID, event *agentv1.PoseTelemetryEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.latest[agentID] = LayoutPose{
		AgentID:   agentID,
		LayoutID:  event.LayoutId,
		X:         event.X,
		Y:         event.Y,
		Yaw:       event.Yaw,
		Timestamp: event.Timestamp.AsTime(),
	}
	p.history[agentID] = append(p.history[agentID], p.latest[agentID])
}

func (p *PoseListener) GetLatestPose(agentID AgentID) LayoutPose {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.latest[agentID]
}

func (p *PoseListener) GetHistory(agentID AgentID) []LayoutPose {
	p.mu.Lock()
	defer p.mu.Unlock()
	src := p.history[agentID]
	out := make([]LayoutPose, len(src))
	copy(out, src)
	return out
}

// cleanHistory removes old pose history that is older than the keepFor duration.
func (p *PoseListener) cleanHistory() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for agentID, poses := range p.history {
		var filtered []LayoutPose
		for _, pose := range poses {
			if time.Since(pose.Timestamp) < p.keepFor {
				filtered = append(filtered, pose)
			}
		}
		p.history[agentID] = filtered
	}
}
