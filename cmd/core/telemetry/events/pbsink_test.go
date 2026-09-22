package events

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"

	// Migrations register themselves via init() side effects -- imported
	// here (not by this package's non-test code) purely so this test can
	// bootstrap a real app with the actual agent_events collection, the
	// same way cmd/core's own tests do.
	_ "vantageos-core/cmd/core/migrations"
	"vantageos-core/cmd/core/telemetry/validate"
)

func bootstrapTestApp(t *testing.T) *pocketbase.PocketBase {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err := app.RunAppMigrations(); err != nil {
		t.Fatalf("RunAppMigrations: %v", err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	return app
}

func TestPocketBaseSinkEmitWritesARow(t *testing.T) {
	app := bootstrapTestApp(t)
	sink := NewPocketBaseSink(app)

	v := validate.Violation{
		AgentID:    "smallbot",
		GroupID:    "group1",
		SchemaHash: "abc123",
		Kind:       validate.KindMissingRequired,
		Paths:      []string{"/status"},
		Detail:     "missing required field(s): status",
		Sample:     []byte(`{"battery_percent":55}`),
	}
	first := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	last := time.Now().UTC().Truncate(time.Second)

	sink.Emit(v, 42, first, last)

	records, err := app.FindAllRecords(AgentEventsCollection)
	if err != nil {
		t.Fatalf("FindAllRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d agent_events rows, want 1", len(records))
	}

	r := records[0]
	if got := r.GetString("agent_id"); got != "smallbot" {
		t.Errorf("agent_id = %q, want smallbot", got)
	}
	if got := r.GetString("type"); got != TypeTelemetryViolation {
		t.Errorf("type = %q, want %q", got, TypeTelemetryViolation)
	}
	if got := r.GetString("severity"); got != "warning" {
		t.Errorf("severity = %q, want warning", got)
	}
	if got := r.GetString("kind"); got != string(validate.KindMissingRequired) {
		t.Errorf("kind = %q, want %q", got, validate.KindMissingRequired)
	}
	if got := r.GetString("signature"); got != v.Signature() {
		t.Errorf("signature = %q, want %q", got, v.Signature())
	}
	if got := r.GetInt("count"); got != 42 {
		t.Errorf("count = %d, want 42", got)
	}
	if got := r.GetDateTime("first_seen").Time().Unix(); got != first.Unix() {
		t.Errorf("first_seen = %v, want %v", got, first.Unix())
	}
	if got := r.GetDateTime("last_seen").Time().Unix(); got != last.Unix() {
		t.Errorf("last_seen = %v, want %v", got, last.Unix())
	}
}

func TestPocketBaseSinkEmitNoContractIsInfoSeverity(t *testing.T) {
	app := bootstrapTestApp(t)
	sink := NewPocketBaseSink(app)

	sink.Emit(validate.Violation{AgentID: "smallbot", Kind: validate.KindNoContract}, 1, time.Now(), time.Now())

	records, err := app.FindAllRecords(AgentEventsCollection)
	if err != nil {
		t.Fatalf("FindAllRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d rows, want 1", len(records))
	}
	if got := records[0].GetString("severity"); got != "info" {
		t.Errorf("severity = %q, want info for no_contract", got)
	}
}

func TestThrottlerWithPocketBaseSinkEndToEnd(t *testing.T) {
	app := bootstrapTestApp(t)
	sink := NewPocketBaseSink(app)
	th := New(sink, Config{Window: 30 * time.Millisecond, PerAgentCap: 50})

	v := validate.Violation{AgentID: "smallbot", Kind: validate.KindTypeMismatch, Paths: []string{"/battery_percent"}}
	for i := 0; i < 500; i++ {
		th.Record(v)
	}

	deadline := time.After(2 * time.Second)
	for {
		records, err := app.FindAllRecords(AgentEventsCollection)
		if err != nil {
			t.Fatalf("FindAllRecords: %v", err)
		}
		if len(records) >= 2 {
			if len(records) > 2 {
				t.Fatalf("got %d rows for a 500-message burst of one signature, want <=2", len(records))
			}
			var sawFirst, sawSummary bool
			for _, r := range records {
				switch r.GetInt("count") {
				case 1:
					sawFirst = true
				case 500:
					sawSummary = true
				}
			}
			if !sawFirst || !sawSummary {
				t.Fatalf("rows = %+v, want one with count=1 and one with count=500", records)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("got %d agent_events rows after 2s, want 2", len(records))
		case <-time.After(5 * time.Millisecond):
		}
	}
}
