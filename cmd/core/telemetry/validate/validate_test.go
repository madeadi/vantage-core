package validate

import (
	"encoding/json"
	"strings"
	"testing"

	"vantageos-core/cmd/core/telemetry/registry"
	"vantageos-core/pkg/agentsdk"
)

const telemetrySchema = `{
	"type": "object",
	"properties": {
		"battery_percent": {"type": "number"},
		"status": {"type": "string", "enum": ["idle", "moving", "charging"]}
	},
	"required": ["battery_percent", "status"]
}`

func newTestValidator(t *testing.T, agentID, groupID, schema string) *Validator {
	t.Helper()
	reg := registry.New()
	if groupID != "" {
		reg.SetAgentGroups(map[string]string{agentID: groupID})
	}
	if groupID != "" && schema != "" {
		if err := reg.SetGroupSchemas(map[string]json.RawMessage{groupID: json.RawMessage(schema)}); err != nil {
			t.Fatalf("SetGroupSchemas: %v", err)
		}
	}
	return New(reg)
}

func TestValidateHappyPath(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	violation := v.Validate("smallbot", []byte(`{"battery_percent":55,"status":"idle"}`))
	if violation != nil {
		t.Fatalf("Validate(valid payload) = %+v, want nil", violation)
	}
}

func TestValidateNoContractNoGroup(t *testing.T) {
	v := newTestValidator(t, "smallbot", "", "")

	violation := v.Validate("smallbot", []byte(`{"anything":"goes"}`))
	if violation == nil {
		t.Fatal("Validate for an agent with no group: want a *Violation, got nil")
	}
	if violation.Kind != KindNoContract {
		t.Errorf("Kind = %q, want %q", violation.Kind, KindNoContract)
	}
	if violation.GroupID != "" {
		t.Errorf("GroupID = %q, want empty", violation.GroupID)
	}
}

func TestValidateNoContractGroupHasNoSchema(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", "") // group exists, no schema set

	violation := v.Validate("smallbot", []byte(`{"anything":"goes"}`))
	if violation == nil {
		t.Fatal("Validate for a group with no schema: want a *Violation, got nil")
	}
	if violation.Kind != KindNoContract {
		t.Errorf("Kind = %q, want %q", violation.Kind, KindNoContract)
	}
}

func TestValidateNotJSON(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	violation := v.Validate("smallbot", []byte(`not json at all`))
	if violation == nil {
		t.Fatal("Validate(invalid JSON): want a *Violation, got nil")
	}
	if violation.Kind != KindNotJSON {
		t.Errorf("Kind = %q, want %q", violation.Kind, KindNotJSON)
	}
	if violation.GroupID != "group1" {
		t.Errorf("GroupID = %q, want group1 (contract was resolved before the parse failed)", violation.GroupID)
	}

	// Sample must still be a legal JSON value even though the input wasn't
	// valid JSON -- see sampleFor.
	if !json.Valid(violation.Sample) {
		t.Errorf("Sample is not valid JSON: %s", violation.Sample)
	}
}

func TestValidateMissingRequired(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	violation := v.Validate("smallbot", []byte(`{"battery_percent":55}`))
	if violation == nil {
		t.Fatal("Validate(missing status): want a *Violation, got nil")
	}
	if violation.Kind != KindMissingRequired {
		t.Fatalf("Kind = %q, want %q", violation.Kind, KindMissingRequired)
	}
	if len(violation.Paths) != 1 || violation.Paths[0] != "/status" {
		t.Errorf("Paths = %v, want [/status]", violation.Paths)
	}
}

func TestValidateMissingRequiredMultipleFields(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	violation := v.Validate("smallbot", []byte(`{}`))
	if violation == nil || violation.Kind != KindMissingRequired {
		t.Fatalf("Validate({}) = %+v, want KindMissingRequired", violation)
	}
	if len(violation.Paths) != 2 {
		t.Fatalf("Paths = %v, want 2 entries", violation.Paths)
	}
	// pathsFor sorts, so this order is deterministic.
	want := []string{"/battery_percent", "/status"}
	for i, p := range want {
		if violation.Paths[i] != p {
			t.Errorf("Paths[%d] = %q, want %q", i, violation.Paths[i], p)
		}
	}
}

func TestValidateTypeMismatch(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	violation := v.Validate("smallbot", []byte(`{"battery_percent":"not a number","status":"idle"}`))
	if violation == nil {
		t.Fatal("Validate(wrong type): want a *Violation, got nil")
	}
	if violation.Kind != KindTypeMismatch {
		t.Fatalf("Kind = %q, want %q", violation.Kind, KindTypeMismatch)
	}
	if len(violation.Paths) != 1 || violation.Paths[0] != "/battery_percent" {
		t.Errorf("Paths = %v, want [/battery_percent]", violation.Paths)
	}
}

func TestValidateConstraintFallback(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	// Both fields present with the right JSON type, but "status" fails the
	// enum constraint -- not a missing_required or type_mismatch case.
	violation := v.Validate("smallbot", []byte(`{"battery_percent":55,"status":"flying"}`))
	if violation == nil {
		t.Fatal("Validate(enum violation): want a *Violation, got nil")
	}
	if violation.Kind != KindConstraint {
		t.Fatalf("Kind = %q, want %q", violation.Kind, KindConstraint)
	}
	if violation.Detail == "" {
		t.Error("Detail is empty, want the underlying schema library's error message")
	}
}

func TestValidateIntegerAcceptedForIntegerTypedNumber(t *testing.T) {
	schema := `{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"]}`
	v := newTestValidator(t, "smallbot", "group1", schema)

	if violation := v.Validate("smallbot", []byte(`{"count":5}`)); violation != nil {
		t.Errorf("Validate(count=5) = %+v, want nil (5 is a whole number, satisfies integer)", violation)
	}
	if violation := v.Validate("smallbot", []byte(`{"count":5.5}`)); violation == nil {
		t.Error("Validate(count=5.5): want a violation, 5.5 is not an integer")
	}
}

func TestCheckSchemaAnnouncementMatch(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	hash, _, err := agentsdk.HashSchema(json.RawMessage(telemetrySchema))
	if err != nil {
		t.Fatalf("HashSchema: %v", err)
	}
	announcement, _ := json.Marshal(agentsdk.TelemetrySchemaAnnouncement{
		AgentID:    "smallbot",
		SchemaHash: hash,
	})

	if violation := v.CheckSchemaAnnouncement("smallbot", announcement); violation != nil {
		t.Errorf("CheckSchemaAnnouncement(matching hash) = %+v, want nil", violation)
	}
}

func TestCheckSchemaAnnouncementMismatch(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	announcement, _ := json.Marshal(agentsdk.TelemetrySchemaAnnouncement{
		AgentID:    "smallbot",
		SchemaHash: "totally-different-hash",
	})

	violation := v.CheckSchemaAnnouncement("smallbot", announcement)
	if violation == nil {
		t.Fatal("CheckSchemaAnnouncement(mismatched hash): want a *Violation, got nil")
	}
	if violation.Kind != KindSchemaContractMismatch {
		t.Errorf("Kind = %q, want %q", violation.Kind, KindSchemaContractMismatch)
	}
}

func TestCheckSchemaAnnouncementNoContract(t *testing.T) {
	v := newTestValidator(t, "smallbot", "", "")

	announcement, _ := json.Marshal(agentsdk.TelemetrySchemaAnnouncement{AgentID: "smallbot", SchemaHash: "whatever"})
	violation := v.CheckSchemaAnnouncement("smallbot", announcement)
	if violation == nil || violation.Kind != KindNoContract {
		t.Fatalf("CheckSchemaAnnouncement for an agent with no group = %+v, want KindNoContract", violation)
	}
}

func TestSignatureStableAcrossRepeatedIdenticalFailures(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	// Same bug, different offending values and different Detail text --
	// Signature must still collapse these to the same key, since that's
	// what lets Step 9's throttle recognize "the same wrong field, again."
	first := v.Validate("smallbot", []byte(`{"battery_percent":"nope","status":"idle"}`))
	second := v.Validate("smallbot", []byte(`{"battery_percent":"still not a number","status":"moving"}`))

	if first == nil || second == nil {
		t.Fatalf("expected both to be violations: first=%v second=%v", first, second)
	}
	if first.Signature() != second.Signature() {
		t.Errorf("Signature() differs for repeated identical failures: %q vs %q", first.Signature(), second.Signature())
	}
}

func TestSignatureDiffersByKindAndPaths(t *testing.T) {
	v := newTestValidator(t, "smallbot", "group1", telemetrySchema)

	missingStatus := v.Validate("smallbot", []byte(`{"battery_percent":55}`))
	missingBattery := v.Validate("smallbot", []byte(`{"status":"idle"}`))
	wrongType := v.Validate("smallbot", []byte(`{"battery_percent":"x","status":"idle"}`))

	sigs := map[string]string{
		"missingStatus":  missingStatus.Signature(),
		"missingBattery": missingBattery.Signature(),
		"wrongType":      wrongType.Signature(),
	}
	seen := make(map[string]string)
	for name, sig := range sigs {
		if other, dup := seen[sig]; dup {
			t.Errorf("%s and %s produced the same signature %q, want distinct signatures for distinct failures", name, other, sig)
		}
		seen[sig] = name
	}
}

func TestSignatureIsDeterministicRegardlessOfPathOrder(t *testing.T) {
	v1 := Violation{Kind: KindMissingRequired, Paths: []string{"/b", "/a"}}
	v2 := Violation{Kind: KindMissingRequired, Paths: []string{"/a", "/b"}}
	if v1.Signature() != v2.Signature() {
		t.Errorf("Signature() depends on Paths order: %q vs %q, want order-independent", v1.Signature(), v2.Signature())
	}
}

func TestSampleForSmallValidJSONPassesThroughUnchanged(t *testing.T) {
	payload := []byte(`{"a":1}`)
	got := sampleFor(payload)
	if string(got) != string(payload) {
		t.Errorf("sampleFor(%s) = %s, want unchanged", payload, got)
	}
}

func TestSampleForInvalidJSONIsWrappedAsString(t *testing.T) {
	payload := []byte(`not json`)
	got := sampleFor(payload)
	if !json.Valid(got) {
		t.Fatalf("sampleFor(invalid JSON) produced invalid JSON: %s", got)
	}
	var s string
	if err := json.Unmarshal(got, &s); err != nil {
		t.Fatalf("sampleFor(invalid JSON) did not produce a JSON string: %s", got)
	}
	if s != "not json" {
		t.Errorf("unwrapped sample = %q, want %q", s, "not json")
	}
}

func TestSampleForOversizedPayloadIsTruncatedAndWrapped(t *testing.T) {
	huge := []byte(`{"x":"` + strings.Repeat("a", maxSampleBytes*2) + `"}`)
	got := sampleFor(huge)
	if !json.Valid(got) {
		t.Fatalf("sampleFor(oversized) produced invalid JSON")
	}
	if len(got) >= len(huge) {
		t.Errorf("sampleFor(oversized) did not shrink the payload: got %d bytes, original %d", len(got), len(huge))
	}
}
