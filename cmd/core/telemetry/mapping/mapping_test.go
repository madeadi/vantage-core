package mapping

import (
	"strconv"
	"testing"
	"time"

	"vantageos-core/cmd/core/telemetry/validate"
)

func TestApplyNilSpecFallsBackToDefaults(t *testing.T) {
	receivedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	result, violation := Apply(nil, "topic-agent", "", receivedAt, []byte(`{"anything":"goes"}`))

	if violation != nil {
		t.Fatalf("Apply(nil spec) violation = %+v, want nil", violation)
	}
	if result.AgentID != "topic-agent" {
		t.Errorf("AgentID = %q, want %q (topic fallback)", result.AgentID, "topic-agent")
	}
	if !result.Time.Equal(receivedAt) {
		t.Errorf("Time = %v, want %v (receivedAt fallback)", result.Time, receivedAt)
	}
	if !result.ReceivedAt.Equal(receivedAt) {
		t.Errorf("ReceivedAt = %v, want %v", result.ReceivedAt, receivedAt)
	}
	if result.Fields != nil {
		t.Errorf("Fields = %v, want nil", result.Fields)
	}
}

func TestApplyEmptySpecFallsBackToDefaults(t *testing.T) {
	receivedAt := time.Now()
	result, violation := Apply(&Spec{}, "topic-agent", "", receivedAt, []byte(`{"x":1}`))

	if violation != nil {
		t.Fatalf("violation = %+v, want nil", violation)
	}
	if result.AgentID != "topic-agent" {
		t.Errorf("AgentID = %q, want topic-agent", result.AgentID)
	}
	if !result.Time.Equal(receivedAt) {
		t.Errorf("Time = %v, want receivedAt %v", result.Time, receivedAt)
	}
}

func TestApplyAgentIDFromPayload(t *testing.T) {
	spec := &Spec{AgentID: "$.robot.serial"}
	result, violation := Apply(spec, "topic-agent", "", time.Now(), []byte(`{"robot":{"serial":"sn-42"}}`))

	if violation != nil {
		t.Fatalf("violation = %+v, want nil", violation)
	}
	if result.AgentID != "sn-42" {
		t.Errorf("AgentID = %q, want sn-42", result.AgentID)
	}
}

func TestApplyAgentIDPathMissingFallsBackSilently(t *testing.T) {
	spec := &Spec{AgentID: "$.robot.serial"}
	result, violation := Apply(spec, "topic-agent", "", time.Now(), []byte(`{"unrelated":true}`))

	if violation != nil {
		t.Fatalf("a missing agent_id path must not raise a violation (topic fallback is reliable): got %+v", violation)
	}
	if result.AgentID != "topic-agent" {
		t.Errorf("AgentID = %q, want topic-agent (fallback)", result.AgentID)
	}
}

func TestApplyAgentIDWrongTypeFallsBackSilently(t *testing.T) {
	spec := &Spec{AgentID: "$.robot.serial"}
	result, _ := Apply(spec, "topic-agent", "", time.Now(), []byte(`{"robot":{"serial":12345}}`))

	if result.AgentID != "topic-agent" {
		t.Errorf("AgentID = %q, want topic-agent (a non-string serial must not be used)", result.AgentID)
	}
}

func TestApplyTimestampRFC3339Explicit(t *testing.T) {
	spec := &Spec{Timestamp: "$.header.stamp", TimestampFormat: "rfc3339"}
	receivedAt := time.Now()
	want := receivedAt.Add(-time.Minute).Truncate(time.Second).UTC()

	payload := []byte(`{"header":{"stamp":"` + want.Format(time.RFC3339) + `"}}`)
	result, violation := Apply(spec, "topic-agent", "", receivedAt, payload)

	if violation != nil {
		t.Fatalf("violation = %+v, want nil", violation)
	}
	if !result.Time.Equal(want) {
		t.Errorf("Time = %v, want %v", result.Time, want)
	}
}

func TestApplyTimestampRFC3339DefaultsWhenFormatUnset(t *testing.T) {
	spec := &Spec{Timestamp: "$.header.stamp"} // TimestampFormat left empty
	receivedAt := time.Now()
	want := receivedAt.Add(-time.Minute).Truncate(time.Second).UTC()

	payload := []byte(`{"header":{"stamp":"` + want.Format(time.RFC3339) + `"}}`)
	result, violation := Apply(spec, "topic-agent", "", receivedAt, payload)

	if violation != nil {
		t.Fatalf("violation = %+v, want nil", violation)
	}
	if !result.Time.Equal(want) {
		t.Errorf("Time = %v, want %v (rfc3339 should be the default format)", result.Time, want)
	}
}

func TestApplyTimestampUnixMillis(t *testing.T) {
	spec := &Spec{Timestamp: "$.stamp", TimestampFormat: "unix_ms"}
	receivedAt := time.Now()
	want := receivedAt.Add(-time.Minute).Truncate(time.Millisecond).UTC()

	payload := []byte(`{"stamp":` + strconv.FormatInt(want.UnixMilli(), 10) + `}`)
	result, violation := Apply(spec, "topic-agent", "", receivedAt, payload)

	if violation != nil {
		t.Fatalf("violation = %+v, want nil", violation)
	}
	if !result.Time.Equal(want) {
		t.Errorf("Time = %v, want %v", result.Time, want)
	}
}

func TestApplyTimestampUnixSeconds(t *testing.T) {
	spec := &Spec{Timestamp: "$.stamp", TimestampFormat: "unix_s"}
	receivedAt := time.Now()
	want := receivedAt.Add(-time.Minute).Truncate(time.Second).UTC()

	payload := []byte(`{"stamp":` + strconv.FormatInt(want.Unix(), 10) + `}`)
	result, violation := Apply(spec, "topic-agent", "", receivedAt, payload)

	if violation != nil {
		t.Fatalf("violation = %+v, want nil", violation)
	}
	if !result.Time.Equal(want) {
		t.Errorf("Time = %v, want %v", result.Time, want)
	}
}

func TestApplyTimestampPathMissingFallsBackSilently(t *testing.T) {
	spec := &Spec{Timestamp: "$.header.stamp"}
	receivedAt := time.Now()
	result, violation := Apply(spec, "topic-agent", "", receivedAt, []byte(`{"unrelated":true}`))

	if violation != nil {
		t.Fatalf("a missing timestamp path must not raise a violation: got %+v", violation)
	}
	if !result.Time.Equal(receivedAt) {
		t.Errorf("Time = %v, want receivedAt %v (fallback)", result.Time, receivedAt)
	}
}

func TestApplyTimestampWrongTypeRaisesBadTimestamp(t *testing.T) {
	spec := &Spec{Timestamp: "$.stamp", TimestampFormat: "rfc3339"}
	receivedAt := time.Now()
	// stamp is a number, but rfc3339 expects a string.
	result, violation := Apply(spec, "topic-agent", "group1", receivedAt, []byte(`{"stamp":12345}`))

	if violation == nil {
		t.Fatal("Apply with a type-mismatched timestamp: want a *validate.Violation, got nil")
	}
	if violation.Kind != validate.KindBadTimestamp {
		t.Errorf("Kind = %q, want %q", violation.Kind, validate.KindBadTimestamp)
	}
	if violation.GroupID != "group1" {
		t.Errorf("GroupID = %q, want group1", violation.GroupID)
	}
	if !result.Time.Equal(receivedAt) {
		t.Errorf("Time = %v, want receivedAt %v (must fall back, never write garbage)", result.Time, receivedAt)
	}
}

func TestApplyTimestampUnparseableStringRaisesBadTimestamp(t *testing.T) {
	spec := &Spec{Timestamp: "$.stamp", TimestampFormat: "rfc3339"}
	receivedAt := time.Now()
	result, violation := Apply(spec, "topic-agent", "", receivedAt, []byte(`{"stamp":"not a timestamp"}`))

	if violation == nil {
		t.Fatal("Apply with an unparseable timestamp string: want a violation, got nil")
	}
	if violation.Kind != validate.KindBadTimestamp {
		t.Errorf("Kind = %q, want %q", violation.Kind, validate.KindBadTimestamp)
	}
	if !result.Time.Equal(receivedAt) {
		t.Error("Time must fall back to receivedAt on an unparseable timestamp")
	}
}

func TestApplyTimestampOutsideSaneWindowRaisesBadTimestamp(t *testing.T) {
	spec := &Spec{Timestamp: "$.stamp", TimestampFormat: "unix_s"}
	receivedAt := time.Now()
	farFuture := receivedAt.Add(48 * time.Hour) // outside the +/-24h window

	payload := []byte(`{"stamp":` + strconv.FormatInt(farFuture.Unix(), 10) + `}`)
	result, violation := Apply(spec, "topic-agent", "", receivedAt, payload)

	if violation == nil {
		t.Fatal("Apply with a timestamp 48h from receive time: want a violation, got nil")
	}
	if violation.Kind != validate.KindBadTimestamp {
		t.Errorf("Kind = %q, want %q", violation.Kind, validate.KindBadTimestamp)
	}
	if !result.Time.Equal(receivedAt) {
		t.Error("Time must fall back to receivedAt when the mapped timestamp is out of the sane window")
	}
}

func TestApplyTimestampWithinSaneWindowIsAccepted(t *testing.T) {
	spec := &Spec{Timestamp: "$.stamp", TimestampFormat: "unix_s"}
	receivedAt := time.Now()
	within := receivedAt.Add(23 * time.Hour) // just inside +/-24h

	payload := []byte(`{"stamp":` + strconv.FormatInt(within.Unix(), 10) + `}`)
	_, violation := Apply(spec, "topic-agent", "", receivedAt, payload)

	if violation != nil {
		t.Errorf("a timestamp within the sane window raised a violation: %+v", violation)
	}
}

func TestApplyUnknownTimestampFormatRaisesBadTimestamp(t *testing.T) {
	spec := &Spec{Timestamp: "$.stamp", TimestampFormat: "banana"}
	_, violation := Apply(spec, "topic-agent", "", time.Now(), []byte(`{"stamp":"2026-01-01T00:00:00Z"}`))

	if violation == nil || violation.Kind != validate.KindBadTimestamp {
		t.Fatalf("Apply with an unknown timestamp_format: want KindBadTimestamp, got %+v", violation)
	}
}

func TestApplyFieldsProjection(t *testing.T) {
	spec := &Spec{Fields: map[string]string{
		"x": "$.pose.position.x",
		"y": "$.pose.position.y",
	}}
	payload := []byte(`{"pose":{"position":{"x":1.5,"y":2.5}}}`)
	result, violation := Apply(spec, "topic-agent", "", time.Now(), payload)

	if violation != nil {
		t.Fatalf("violation = %+v, want nil", violation)
	}
	if result.Fields["x"] != 1.5 || result.Fields["y"] != 2.5 {
		t.Errorf("Fields = %v, want {x:1.5 y:2.5}", result.Fields)
	}
}

func TestApplyFieldsProjectionMissingPathOmitsThatField(t *testing.T) {
	spec := &Spec{Fields: map[string]string{"x": "$.pose.x", "missing": "$.nope"}}
	payload := []byte(`{"pose":{"x":1.5}}`)
	result, _ := Apply(spec, "topic-agent", "", time.Now(), payload)

	if _, ok := result.Fields["missing"]; ok {
		t.Error("Fields contains an entry for a path absent from the payload, want it omitted")
	}
	if result.Fields["x"] != 1.5 {
		t.Errorf(`Fields["x"] = %v, want 1.5`, result.Fields["x"])
	}
}

func TestApplyMalformedPayloadFallsBackToDefaultsWithoutError(t *testing.T) {
	spec := &Spec{AgentID: "$.robot.serial", Timestamp: "$.stamp"}
	receivedAt := time.Now()
	result, violation := Apply(spec, "topic-agent", "", receivedAt, []byte(`not json`))

	// Reporting invalid JSON is validate.Validate's job (KindNotJSON), not
	// this layer's -- Apply must degrade gracefully to defaults.
	if violation != nil {
		t.Errorf("violation = %+v, want nil (not_json is validate's concern, not mapping's)", violation)
	}
	if result.AgentID != "topic-agent" || !result.Time.Equal(receivedAt) {
		t.Errorf("result = %+v, want defaults", result)
	}
}

func TestExtractDottedPath(t *testing.T) {
	payload := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": "deep value",
			},
		},
	}

	cases := []struct {
		path   string
		want   any
		wantOK bool
	}{
		{"$.a.b.c", "deep value", true},
		{"a.b.c", "deep value", true}, // "$." prefix is optional
		{"$.a.b.missing", nil, false},
		{"$.a.b.c.d", nil, false}, // c is a string, cannot descend further
		{"$.nope", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, ok := extract(payload, tc.path)
			if ok != tc.wantOK {
				t.Fatalf("extract(%q) ok = %v, want %v", tc.path, ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Errorf("extract(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"$.header.stamp": "/header/stamp",
		"header.stamp":   "/header/stamp",
		"$.x":            "/x",
	}
	for in, want := range cases {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}
