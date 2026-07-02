package main

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"vantageos-core/internal/core/model"
	"vantageos-core/internal/core/repository"
	"vantageos-core/internal/core/service"
	"vantageos-core/pkg/agentsdk"
)

//go:embed web
var webFS embed.FS

type AgentWithPose struct {
	model.Agent
	Pose    model.LayoutPose
	Cameras []agentsdk.CameraConfig
}

type TaskView struct {
	ID         string
	Type       string
	Status     model.TaskStatus
	AgentName  string
	ReceivedAt time.Time
}

type RegistryStats struct {
	ConnectedAgents   int                   `json:"connected_agents"`
	TotalAgents       int                   `json:"total_agents"`
	Agents            []AgentWithPose       `json:"agents"`
	RecentTasks       []TaskView            `json:"recent_tasks"`
	ConnectedMissions int                   `json:"connected_missions"`
	TotalMissions     int                   `json:"total_missions"`
	Missions          []service.MissionInfo `json:"missions"`
}

type UI struct {
	mu           sync.RWMutex
	ar           *service.AgentRegistry
	taskRepo     repository.TaskRepo
	mr           *service.MissionRegistry
	poseListener *service.PoseListener
}

func NewUI(ar *service.AgentRegistry, taskRepo repository.TaskRepo, mr *service.MissionRegistry, poseListener *service.PoseListener) *UI {
	return &UI{ar: ar, taskRepo: taskRepo, mr: mr, poseListener: poseListener}
}

func (r *UI) Stats() RegistryStats {
	r.mu.RLock()

	onlineAgents := r.ar.OnlineAgents()

	agents := make([]AgentWithPose, 0, len(onlineAgents))
	for _, a := range onlineAgents {
		agents = append(agents, AgentWithPose{
			Agent:   *a,
			Pose:    r.poseListener.GetLatestPose(a.ID),
			Cameras: r.ar.GetCameras(a.ID),
		})
	}
	connected := len(onlineAgents)
	total := len(r.ar.AllowedAgents())
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
			AgentName:  r.ar.NameFor(t.AgentID),
			ReceivedAt: t.ReceivedAt,
		})
	}

	missions := r.mr.ListMissions()
	connectedMissions := 0
	for _, m := range missions {
		if m.Online {
			connectedMissions++
		}
	}

	return RegistryStats{
		ConnectedAgents:   connected,
		TotalAgents:       total,
		Agents:            agents,
		RecentTasks:       tasks,
		ConnectedMissions: connectedMissions,
		TotalMissions:     len(missions),
		Missions:          missions,
	}
}

func (r *UI) RegisterUIRoutes(mux *http.ServeMux) {
	sub, _ := fs.Sub(webFS, "web")
	mux.HandleFunc("GET /ui/events", r.handleSSE)
	mux.Handle("/ui/", http.StripPrefix("/ui", http.FileServer(http.FS(sub))))
}

func (r *UI) handleSSE(w http.ResponseWriter, req *http.Request) {
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

func taskStatusLabel(s model.TaskStatus) string {
	switch s {
	case model.TaskStatusDraft:
		return "draft"
	case model.TaskStatusStarting:
		return "starting"
	case model.TaskStatusCannotStart:
		return "cannot start"
	case model.TaskStatusStarted:
		return "started"
	case model.TaskStatusExpiring:
		return "expiring"
	case model.TaskStatusExpired:
		return "expired"
	case model.TaskStatusAborting:
		return "aborting"
	case model.TaskStatusAborted:
		return "aborted"
	case model.TaskStatusFailed:
		return "failed"
	case model.TaskStatusFinishing:
		return "finishing"
	case model.TaskStatusFinished:
		return "finished"
	default:
		return "—"
	}
}

func taskStatusClass(s model.TaskStatus) string {
	switch s {
	case model.TaskStatusStarted, model.TaskStatusFinishing, model.TaskStatusFinished:
		return "status-ok"
	case model.TaskStatusFailed, model.TaskStatusExpired, model.TaskStatusAborted, model.TaskStatusCannotStart:
		return "status-err"
	case model.TaskStatusStarting, model.TaskStatusExpiring, model.TaskStatusAborting:
		return "status-warn"
	default:
		return "status-dim"
	}
}

// buildCamerasHTML renders thumbnails for an agent's mjpg cameras.
// rtsp and webrtc cameras aren't playable directly in an <img> tag, so they're skipped for now.
func buildCamerasHTML(cameras []agentsdk.CameraConfig) string {
	var b strings.Builder
	hasMJpg := false
	for _, c := range cameras {
		if c.Type != agentsdk.CameraTypeMJpg {
			continue
		}
		if !hasMJpg {
			b.WriteString(`<div class="agent-cameras">`)
			hasMJpg = true
		}
		fmt.Fprintf(&b,
			`<img class="camera-feed" src="%s" alt="%s" loading="lazy">`,
			html.EscapeString(c.Url), html.EscapeString(c.CameraID),
		)
	}
	if hasMJpg {
		b.WriteString(`</div>`)
	}
	return b.String()
}

func buildStatsFragment(s RegistryStats) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		`<div class="grid"><div class="card"><div class="card-value">%d</div><div class="card-label">Agents Connected</div></div><div class="card"><div class="card-value">%d</div><div class="card-label">Agents Registered</div></div><div class="card"><div class="card-value">%d</div><div class="card-label">Missions Online</div></div><div class="card"><div class="card-value">%d</div><div class="card-label">Missions Registered</div></div></div>`,
		s.ConnectedAgents, s.TotalAgents, s.ConnectedMissions, s.TotalMissions,
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
				`<li class="agent-item"><div class="agent-item-header"><span class="dot"></span><span class="agent-name">%s</span>%s</div>%s</li>`,
				html.EscapeString(a.Name), poseStr, buildCamerasHTML(a.Cameras),
			)
		}
		b.WriteString(`</ul>`)
	}

	b.WriteString(`<p class="section-title" style="margin-top:1.5rem">Missions</p>`)
	if len(s.Missions) == 0 {
		b.WriteString(`<p class="empty">No missions registered</p>`)
	} else {
		b.WriteString(`<ul class="agent-list">`)
		for _, m := range s.Missions {
			dotClass := "dot"
			if !m.Online {
				dotClass = "dot dot-offline"
			}
			name := m.Name
			if name == "" {
				name = string(m.ID)
			}
			fmt.Fprintf(&b,
				`<li class="agent-item"><div class="agent-item-header"><span class="%s"></span><span class="agent-name">%s</span></div></li>`,
				dotClass, html.EscapeString(name),
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
