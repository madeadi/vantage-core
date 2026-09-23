package controller

import (
	"fmt"
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"vantageos-core/cmd/core/telemetry/live"
)

// TelemetryLiveController serves GET /telemetry/live?agent_id= over SSE,
// fed by live.Broadcaster (populated by cmd/core/telemetry.go's ingest
// listener). See specs/mqtt_telemetry.specs.md Step 13: SSE rather than
// PocketBase realtime because telemetry does not live in PocketBase.
type TelemetryLiveController struct {
	app         core.App
	broadcaster *live.Broadcaster
}

func NewTelemetryLiveController(app core.App, broadcaster *live.Broadcaster) *TelemetryLiveController {
	return &TelemetryLiveController{app: app, broadcaster: broadcaster}
}

func (c *TelemetryLiveController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /telemetry/live", c.handleLive)
}

func (c *TelemetryLiveController) handleLive(w http.ResponseWriter, req *http.Request) {
	record, err := authenticatePB(c.app, req.Header)
	if err != nil || !isOperatorOrAbove(record) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	agentID := req.URL.Query().Get("agent_id")
	if agentID == "" {
		http.Error(w, "agent_id is required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch, unsubscribe := c.broadcaster.Subscribe(agentID)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-req.Context().Done():
			return
		case payload := <-ch:
			// Agent telemetry payloads are compact JSON (json.Marshal output,
			// see pkg/agentsdk.Telemetry[T].Publish) with no embedded
			// newlines, so a single "data:" line is safe -- same assumption
			// cmd/core/ui.go's existing SSE handler makes.
			fmt.Fprintf(w, "event: telemetry\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}
