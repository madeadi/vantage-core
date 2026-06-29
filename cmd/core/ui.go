package main

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

type AgentWithPose struct {
	Agent
	Pose LayoutPose
}

type TaskView struct {
	ID          string
	Type        string
	Status      TaskStatus
	AgentName   string
	ReceivedAt  time.Time
}

type RegistryStats struct {
	ConnectedAgents int             `json:"connected_agents"`
	TotalAgents     int             `json:"total_agents"`
	Agents          []AgentWithPose `json:"agents"`
	RecentTasks     []TaskView      `json:"recent_tasks"`
}

func (r *AgentRegistry) Stats() RegistryStats {
	r.mu.RLock()
	agents := make([]AgentWithPose, 0, len(r.onlineAgents))
	for _, a := range r.onlineAgents {
		agents = append(agents, AgentWithPose{
			Agent: *a,
			Pose:  r.poseListener.GetLatestPose(a.ID),
		})
	}
	connected := len(r.onlineAgents)
	total := len(r.allowedAgents)
	r.mu.RUnlock()

	all := r.taskRepo.ListTasks("")
	sort.Slice(all, func(i, j int) bool {
		return all[i].ReceivedAt.After(all[j].ReceivedAt)
	})
	if len(all) > 20 {
		all = all[:20]
	}
	tasks := make([]TaskView, 0, len(all))
	for _, t := range all {
		tasks = append(tasks, TaskView{
			ID:         t.ID,
			Type:       t.Type,
			Status:     t.Status,
			AgentName:  r.nameFor(t.AgentID),
			ReceivedAt: t.ReceivedAt,
		})
	}

	return RegistryStats{
		ConnectedAgents: connected,
		TotalAgents:     total,
		Agents:          agents,
		RecentTasks:     tasks,
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

func taskStatusLabel(s TaskStatus) string {
	switch s {
	case TaskStatusDraft:
		return "draft"
	case TaskStatusStarting:
		return "starting"
	case TaskStatusCannotStart:
		return "cannot start"
	case TaskStatusStarted:
		return "started"
	case TaskStatusExpiring:
		return "expiring"
	case TaskStatusExpired:
		return "expired"
	case TaskStatusAborting:
		return "aborting"
	case TaskStatusAborted:
		return "aborted"
	case TaskStatusFailed:
		return "failed"
	case TaskStatusFinishing:
		return "finishing"
	case TaskStatusFinished:
		return "finished"
	default:
		return "—"
	}
}

func taskStatusClass(s TaskStatus) string {
	switch s {
	case TaskStatusStarted, TaskStatusFinishing, TaskStatusFinished:
		return "status-ok"
	case TaskStatusFailed, TaskStatusExpired, TaskStatusAborted, TaskStatusCannotStart:
		return "status-err"
	case TaskStatusStarting, TaskStatusExpiring, TaskStatusAborting:
		return "status-warn"
	default:
		return "status-dim"
	}
}

func buildStatsFragment(s RegistryStats) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		`<div class="grid"><div class="card"><div class="card-value">%d</div><div class="card-label">Agents Connected</div></div><div class="card"><div class="card-value">%d</div><div class="card-label">Agents Registered</div></div></div>`,
		s.ConnectedAgents, s.TotalAgents,
	)
	b.WriteString(`<div class="columns">`)

	b.WriteString(`<div class="col-agents"><p class="section-title">Connected Agents</p>`)
	if len(s.Agents) == 0 {
		b.WriteString(`<p class="empty">No agents connected</p>`)
	} else {
		b.WriteString(`<ul class="agent-list">`)
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
				`<li class="agent-item"><span class="dot"></span><span class="agent-name">%s</span>%s</li>`,
				html.EscapeString(a.Name), poseStr,
			)
		}
		b.WriteString(`</ul>`)
	}
	b.WriteString(`</div>`)

	b.WriteString(`<div class="col-tasks"><p class="section-title">Recent Tasks</p>`)
	if len(s.RecentTasks) == 0 {
		b.WriteString(`<p class="empty">No tasks yet</p>`)
	} else {
		b.WriteString(`<div class="table-wrap"><table class="task-table"><thead><tr><th>Task ID</th><th>Type</th><th>Status</th><th>Agent</th><th>Submitted</th></tr></thead><tbody>`)
		for _, t := range s.RecentTasks {
			submitted := "—"
			if !t.ReceivedAt.IsZero() {
				submitted = t.ReceivedAt.UTC().Format("2006-01-02 15:04:05")
			}
			agentName := t.AgentName
			if agentName == "" {
				agentName = "—"
			}
			fmt.Fprintf(&b,
				`<tr><td class="task-id">%s</td><td>%s</td><td><span class="status-badge %s">%s</span></td><td>%s</td><td class="ts">%s</td></tr>`,
				html.EscapeString(t.ID[:8]),
				html.EscapeString(t.Type),
				taskStatusClass(t.Status),
				taskStatusLabel(t.Status),
				html.EscapeString(agentName),
				submitted,
			)
		}
		b.WriteString(`</tbody></table></div>`)
	}
	b.WriteString(`</div>`)

	b.WriteString(`</div>`)
	return b.String()
}
