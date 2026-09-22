// Package validate checks ingested telemetry and schema announcements
// against a group's compiled contract (cmd/core/telemetry/registry) and
// reports the result as a structured Violation. See
// specs/mqtt_telemetry.specs.md Step 8.
package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Kind classifies why a Violation was raised.
type Kind string

const (
	// KindMissingRequired: the payload is missing one or more properties
	// its group's schema requires.
	KindMissingRequired Kind = "missing_required"
	// KindTypeMismatch: a property is present but its JSON type doesn't
	// match what the schema declares.
	KindTypeMismatch Kind = "type_mismatch"
	// KindConstraint: any other schema failure (pattern, min/max, enum,
	// nested object/array shape, ...) -- jsonschema-go's Validate reports
	// these as free-text errors with no machine-readable path, so Paths is
	// typically empty for this Kind; Detail carries the library's message.
	KindConstraint Kind = "constraint"
	// KindNotJSON: the payload could not be parsed as JSON at all.
	KindNotJSON Kind = "not_json"
	// KindNoContract: the agent has no group, or its group has no schema
	// set. Not a rejection -- per specs/mqtt_telemetry.specs.md, telemetry
	// with no contract still persists (marked valid=null); this Kind exists
	// so a developer still gets a (throttled) event telling them why
	// nothing is being validated.
	KindNoContract Kind = "no_contract"
	// KindSchemaContractMismatch: an agent's own advisory declared schema
	// (from its telemetry-schema announcement) disagrees with its group's
	// authoritative contract -- the earliest possible warning that an
	// agent's idea of its own shape has drifted, caught at connect time
	// instead of after N rejected messages.
	KindSchemaContractMismatch Kind = "schema_contract_mismatch"
	// KindBadTimestamp is raised by the mapping layer (spec Step 11), not
	// this package. Declared here because Kind is the shared vocabulary
	// every telemetry violation event uses, regardless of which stage
	// raised it.
	KindBadTimestamp Kind = "bad_timestamp"
)

// Violation is a structured validation result -- not just a bool, since the
// developer-facing UI (spec Step 14) needs to show which field is wrong,
// not just that something is.
type Violation struct {
	AgentID    string
	GroupID    string
	SchemaHash string
	Kind       Kind
	// Paths are JSON Pointers into the offending payload, e.g. "/status".
	// Populated for KindMissingRequired and KindTypeMismatch, where the
	// schema library gives enough information to identify them precisely;
	// typically empty for KindConstraint, where it doesn't (see Kind's doc
	// comment).
	Paths  []string
	Detail string
	// Sample is a safe-to-store JSON value derived from the offending
	// payload -- see sampleFor for why this is not always the raw bytes.
	Sample json.RawMessage
}

// Signature is a stable hash of (Kind, sorted Paths) -- the throttle key
// spec Step 9 groups repeated identical failures by, so an agent sending
// the same wrong field 100x/s produces one event, not 100. It deliberately
// excludes Detail and Sample, which vary per occurrence even when the
// underlying bug is the same one.
func (v Violation) Signature() string {
	paths := append([]string(nil), v.Paths...)
	sort.Strings(paths)

	h := sha256.New()
	h.Write([]byte(v.Kind))
	for _, p := range paths {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

const maxSampleBytes = 4000

// sampleFor returns a safe-to-store JSON value for a Violation's Sample.
// payload is returned as-is (a raw JSON value) only when it both fits
// within maxSampleBytes and is itself valid JSON; otherwise it is wrapped
// as a JSON string, since a byte-truncated JSON document, or a payload that
// was never valid JSON in the first place (the not_json case), cannot be
// stored as a raw JSON value.
func sampleFor(payload []byte) json.RawMessage {
	trimmed := payload
	truncated := false
	if len(trimmed) > maxSampleBytes {
		trimmed = trimmed[:maxSampleBytes]
		truncated = true
	}

	if !truncated && json.Valid(trimmed) {
		return json.RawMessage(append([]byte(nil), trimmed...))
	}

	s := string(trimmed)
	if truncated {
		s += "…(truncated)"
	}
	b, _ := json.Marshal(s) // marshaling a string never fails
	return b
}
