package main

import (
	"encoding/json"
	"testing"
	"time"

	"vantageos-core/cmd/core/telemetry/ingest"
	"vantageos-core/cmd/core/telemetry/live"
	"vantageos-core/cmd/core/telemetry/validate"
)

func TestValidPtr(t *testing.T) {
	if got := validPtr(nil); got == nil || *got != true {
		t.Errorf("validPtr(nil) = %v, want *true", got)
	}
	if got := validPtr(&validate.Violation{Kind: validate.KindNoContract}); got != nil {
		t.Errorf("validPtr(no_contract) = %v, want nil", got)
	}
	if got := validPtr(&validate.Violation{Kind: validate.KindMissingRequired}); got == nil || *got != false {
		t.Errorf("validPtr(missing_required) = %v, want *false", got)
	}
}

// TestPublishLiveEnvelopeCarriesValidity is Step 14's Live page
// requirement ("each row marked valid/invalid"): the live SSE feed must
// carry the same validity Step 12 persists, not just the raw payload,
// since the browser has no way to re-validate against the group's JSON
// Schema itself.
func TestPublishLiveEnvelopeCarriesValidity(t *testing.T) {
	b := live.New()
	ch, unsubscribe := b.Subscribe("bot-1")
	defer unsubscribe()

	m := ingest.Message{AgentID: "bot-1", Payload: []byte(`{"battery_percent":50}`), ReceivedAt: time.Now()}
	publishLiveEnvelope(b, m, nil) // nil violation == valid

	select {
	case raw := <-ch:
		var env struct {
			Valid   *bool           `json:"valid"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		if env.Valid == nil || *env.Valid != true {
			t.Errorf("valid = %v, want *true", env.Valid)
		}
		if string(env.Payload) != `{"battery_percent":50}` {
			t.Errorf("payload = %s, want the original payload verbatim", env.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("no envelope published")
	}

	publishLiveEnvelope(b, m, &validate.Violation{Kind: validate.KindNoContract})
	select {
	case raw := <-ch:
		var env struct {
			Valid *bool `json:"valid"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		if env.Valid != nil {
			t.Errorf("valid = %v, want nil for no_contract", *env.Valid)
		}
	case <-time.After(time.Second):
		t.Fatal("no envelope published")
	}
}
