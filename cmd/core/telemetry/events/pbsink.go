package events

import (
	"log/slog"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"vantageos-core/cmd/core/telemetry/validate"
)

// AgentEventsCollection is the PocketBase collection PocketBaseSink writes
// to -- created by cmd/core/migrations/1788900001_add_agent_events_collection.go.
const AgentEventsCollection = "agent_events"

// PocketBaseSink persists throttled events as rows in the agent_events
// collection. A failed save is logged and dropped, not retried -- these are
// best-effort observability events, not data the rest of the system depends
// on to function.
type PocketBaseSink struct {
	app core.App
}

// NewPocketBaseSink returns a Sink writing through app.
func NewPocketBaseSink(app core.App) *PocketBaseSink {
	return &PocketBaseSink{app: app}
}

func (s *PocketBaseSink) Emit(v validate.Violation, count int, firstSeen, lastSeen time.Time) {
	coll, err := s.app.FindCollectionByNameOrId(AgentEventsCollection)
	if err != nil {
		slog.Error("agent_events: collection lookup failed", "err", err)
		return
	}

	r := core.NewRecord(coll)
	r.Set("agent_id", v.AgentID)
	r.Set("type", TypeTelemetryViolation)
	r.Set("severity", severityFor(v.Kind))
	r.Set("kind", string(v.Kind))
	r.Set("signature", v.Signature())
	r.Set("detail", v.Detail)
	if len(v.Paths) > 0 {
		r.Set("paths", v.Paths)
	}
	if len(v.Sample) > 0 {
		r.Set("sample", v.Sample)
	}
	r.Set("count", count)
	r.Set("first_seen", firstSeen)
	r.Set("last_seen", lastSeen)

	if err := s.app.Save(r); err != nil {
		slog.Error("agent_events: save failed", "agent_id", v.AgentID, "kind", v.Kind, "err", err)
	}
}

// severityFor maps a Violation's Kind to the agent_events.severity enum
// (info/warning/error). no_contract is informational -- there is nothing an
// agent developer did wrong, just a fleet configuration gap; every other
// Kind reflects an agent actually sending data its contract rejects.
func severityFor(kind validate.Kind) string {
	if kind == validate.KindNoContract {
		return "info"
	}
	return "warning"
}
