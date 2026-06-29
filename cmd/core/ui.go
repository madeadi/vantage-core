package main

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

type AgentWithPose struct {
	Agent
	Pose LayoutPose
}

type RegistryStats struct {
	ConnectedAgents int             `json:"connected_agents"`
	TotalAgents     int             `json:"total_agents"`
	Agents          []AgentWithPose `json:"agents"`
}

func (r *AgentRegistry) Stats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agents := make([]AgentWithPose, 0, len(r.onlineAgents))
	for _, a := range r.onlineAgents {
		agents = append(agents, AgentWithPose{
			Agent: *a,
			Pose:  r.poseListener.GetLatestPose(a.ID),
		})
	}
	return RegistryStats{
		ConnectedAgents: len(r.onlineAgents),
		TotalAgents:     len(r.allowedAgents),
		Agents:          agents,
	}
}

func (r *AgentRegistry) RegisterUIRoutes(mux *http.ServeMux) {
	sub, _ := fs.Sub(webFS, "web")
	mux.HandleFunc("GET /ui/events", r.handleSSE)
	mux.Handle("/ui/", http.StripPrefix("/ui", http.FileServer(http.FS(sub))))
}

func (r *AgentRegistry) handleSSE(w http.ResponseWriter, req *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeUpdate := func() {
		fmt.Fprintf(w, "event: update\ndata: %s\n\n", buildStatsFragment(r.Stats()))
		flusher.Flush()
	}

	writeUpdate()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-req.Context().Done():
			return
		case <-ticker.C:
			writeUpdate()
		}
	}
}

func buildStatsFragment(s RegistryStats) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		`<div class="grid"><div class="card"><div class="card-value">%d</div><div class="card-label">Agents Connected</div></div><div class="card"><div class="card-value">%d</div><div class="card-label">Agents Registered</div></div></div>`,
		s.ConnectedAgents, s.TotalAgents,
	)
	b.WriteString(`<p class="section-title">Connected Agents</p>`)
	if len(s.Agents) == 0 {
		b.WriteString(`<p class="empty">No agents connected</p>`)
	} else {
		b.WriteString(`<ul class="agentsdk-list">`)
		for _, a := range s.Agents {
			pose := a.Pose
			var poseStr string
			if pose.Timestamp.IsZero() {
				poseStr = `<span class="pose-empty">no pose</span>`
			} else {
				poseStr = fmt.Sprintf(
					`<span class="pose">x&nbsp;%.2f&nbsp; y&nbsp;%.2f&nbsp; yaw&nbsp;%.1f°</span>`,
					pose.X, pose.Y, pose.Yaw,
				)
			}
			fmt.Fprintf(&b,
				`<li class="agentsdk-item"><span class="dot"></span><span class="agentsdk-name">%s</span><span class="agentsdk-id">%s</span>%s</li>`,
				html.EscapeString(a.Name), html.EscapeString(string(a.ID)), poseStr,
			)
		}
		b.WriteString(`</ul>`)
	}
	return b.String()
}
