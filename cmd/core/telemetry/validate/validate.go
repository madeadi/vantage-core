package validate

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/jsonschema-go/jsonschema"

	"vantageos-core/cmd/core/telemetry/registry"
	"vantageos-core/pkg/agentsdk"
)

// Validator checks ingested telemetry and schema announcements against an
// agent's group contract (cmd/core/telemetry/registry).
type Validator struct {
	registry *registry.Registry
}

// New returns a Validator resolving contracts through reg.
func New(reg *registry.Registry) *Validator {
	return &Validator{registry: reg}
}

// Validate checks payload (raw telemetry JSON) for agentID against its
// group's compiled contract. It returns nil if the payload is valid and a
// contract exists; a *Violation otherwise, including the no_contract case
// (not a rejection -- see KindNoContract's doc comment).
func (val *Validator) Validate(agentID string, payload []byte) *Violation {
	entry, ok := val.registry.Resolve(agentID)
	if !ok {
		return &Violation{AgentID: agentID, Kind: KindNoContract, Sample: sampleFor(payload)}
	}

	var instance any
	if err := json.Unmarshal(payload, &instance); err != nil {
		return &Violation{
			AgentID: agentID, GroupID: entry.GroupID, SchemaHash: entry.Hash,
			Kind: KindNotJSON, Detail: err.Error(), Sample: sampleFor(payload),
		}
	}

	if err := entry.Resolved.Validate(instance); err != nil {
		kind, paths, detail := classify(entry.Resolved.Schema(), instance, err)
		return &Violation{
			AgentID: agentID, GroupID: entry.GroupID, SchemaHash: entry.Hash,
			Kind: kind, Paths: paths, Detail: detail, Sample: sampleFor(payload),
		}
	}

	return nil
}

// CheckSchemaAnnouncement checks an agent's advisory declared-schema
// announcement (payload is a marshaled agentsdk.TelemetrySchemaAnnouncement)
// against its group's authoritative contract hash, raising
// schema_contract_mismatch on a difference -- see that Kind's doc comment.
func (val *Validator) CheckSchemaAnnouncement(agentID string, payload []byte) *Violation {
	entry, ok := val.registry.Resolve(agentID)
	if !ok {
		return &Violation{AgentID: agentID, Kind: KindNoContract, Sample: sampleFor(payload)}
	}

	var announcement agentsdk.TelemetrySchemaAnnouncement
	if err := json.Unmarshal(payload, &announcement); err != nil {
		return &Violation{
			AgentID: agentID, GroupID: entry.GroupID, SchemaHash: entry.Hash,
			Kind: KindNotJSON, Detail: err.Error(), Sample: sampleFor(payload),
		}
	}

	if announcement.SchemaHash != entry.Hash {
		return &Violation{
			AgentID: agentID, GroupID: entry.GroupID, SchemaHash: entry.Hash,
			Kind: KindSchemaContractMismatch,
			Detail: fmt.Sprintf("agent declared schema_hash %q, group contract is %q",
				announcement.SchemaHash, entry.Hash),
			Sample: sampleFor(payload),
		}
	}

	return nil
}

// classify turns jsonschema-go's opaque validation error into the most
// specific Kind+Paths it can, checking the two highest-value, most common
// failure modes (a required field missing, a field with the wrong JSON
// type) directly against schema and instance -- jsonschema-go's own error
// carries neither a machine-readable path nor a distinct type per failure
// (see this package's own investigation, recorded in the spec), so this is
// deliberately a shallow, targeted check, not a general JSON Schema error
// analyzer. The real pass/fail decision always comes from
// Resolved.Validate itself; classify only runs after that has already
// failed, to explain it. Anything classify doesn't recognize falls back to
// KindConstraint with the library's own error text as Detail.
func classify(schema *jsonschema.Schema, instance any, validateErr error) (Kind, []string, string) {
	obj, ok := instance.(map[string]any)
	if !ok {
		return KindConstraint, nil, validateErr.Error()
	}

	if missing := missingRequired(schema, obj); len(missing) > 0 {
		return KindMissingRequired, pathsFor(missing),
			fmt.Sprintf("missing required field(s): %s", joinSorted(missing))
	}

	if mismatched := mismatchedTypes(schema, obj); len(mismatched) > 0 {
		return KindTypeMismatch, pathsFor(mismatched),
			fmt.Sprintf("field(s) with the wrong type: %s", joinSorted(mismatched))
	}

	return KindConstraint, nil, validateErr.Error()
}

func missingRequired(schema *jsonschema.Schema, obj map[string]any) []string {
	var missing []string
	for _, name := range schema.Required {
		if _, present := obj[name]; !present {
			missing = append(missing, name)
		}
	}
	return missing
}

func mismatchedTypes(schema *jsonschema.Schema, obj map[string]any) []string {
	var mismatched []string
	for name, propSchema := range schema.Properties {
		v, present := obj[name]
		if !present {
			continue // absence is missing_required's concern, not this one
		}
		if !typeMatches(v, propSchema) {
			mismatched = append(mismatched, name)
		}
	}
	return mismatched
}

// typeMatches reports whether v's JSON type satisfies propSchema's declared
// type(s). An unconstrained schema (no type declared) always matches.
func typeMatches(v any, propSchema *jsonschema.Schema) bool {
	types := propSchema.Types
	if len(types) == 0 && propSchema.Type != "" {
		types = []string{propSchema.Type}
	}
	if len(types) == 0 {
		return true
	}

	got := jsonTypeOf(v)
	for _, want := range types {
		if got == want {
			return true
		}
		if want == "integer" && got == "number" {
			if f, ok := v.(float64); ok && f == float64(int64(f)) {
				return true
			}
		}
	}
	return false
}

func jsonTypeOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

func pathsFor(names []string) []string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	paths := make([]string, len(sorted))
	for i, n := range sorted {
		paths[i] = "/" + n
	}
	return paths
}

func joinSorted(names []string) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	out := ""
	for i, n := range sorted {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
