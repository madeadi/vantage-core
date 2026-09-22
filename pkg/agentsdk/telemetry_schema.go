package agentsdk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// TelemetrySchema is a JSON Schema derived from a Go type, plus a stable
// content hash. Two derivations of the same type always produce the same
// Hash, and two structurally different types (almost) never share one.
type TelemetrySchema struct {
	// Hash is the first 16 hex characters of the sha256 of Schema. Compare
	// hashes instead of re-parsing Schema when checking whether an agent's
	// declared shape still matches what it announced last.
	Hash string
	// Schema is the derived JSON Schema (draft 2020-12), canonicalised to
	// sorted-key JSON so byte-for-byte comparison is meaningful.
	Schema json.RawMessage
}

// DeriveTelemetrySchema derives a JSON Schema for T using jsonschema.For and
// returns it with a stable content hash.
//
// This is advisory only: the authoritative telemetry contract an agent's
// payloads are validated against lives in agent_groups.telemetry_schema in
// PocketBase, not on the wire. What an agent derives and announces here is
// compared against that contract to raise schema_contract_mismatch — the
// earliest possible warning that an agent's own idea of its telemetry shape
// has drifted from what its fleet expects, caught at connect time instead of
// after N rejected messages. See specs/mqtt_telemetry.specs.md.
func DeriveTelemetrySchema[T any]() (TelemetrySchema, error) {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		return TelemetrySchema{}, fmt.Errorf("agentsdk: derive schema: %w", err)
	}

	raw, err := json.Marshal(s)
	if err != nil {
		return TelemetrySchema{}, fmt.Errorf("agentsdk: marshal schema: %w", err)
	}

	hash, canon, err := HashSchema(raw)
	if err != nil {
		return TelemetrySchema{}, err
	}
	return TelemetrySchema{Hash: hash, Schema: canon}, nil
}

// HashSchema canonicalises raw JSON Schema (sorted-key JSON — see
// canonicalizeJSON) and returns it alongside a stable content hash: the
// first 16 hex characters of its sha256.
//
// This is exported, and used on both sides of the schema_contract_mismatch
// comparison in specs/mqtt_telemetry.specs.md Step 8 — DeriveTelemetrySchema
// calls it for an agent's advisory declared schema, and cmd/core's schema
// registry calls it for the authoritative group contract. Both sides must
// hash with the same algorithm for their hashes to be comparable at all;
// duplicating this logic in the registry package would silently break that.
func HashSchema(raw json.RawMessage) (hash string, canonical json.RawMessage, err error) {
	canon, err := canonicalizeJSON(raw)
	if err != nil {
		return "", nil, fmt.Errorf("agentsdk: canonicalize schema: %w", err)
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])[:16], canon, nil
}

// canonicalizeJSON re-encodes raw with object keys sorted, so two
// structurally identical JSON documents produce byte-identical output
// regardless of the source marshaler's own field order. This relies on
// encoding/json.Marshal sorting map[string]any keys lexicographically, which
// is a documented guarantee of the standard library, not an implementation
// detail of *jsonschema.Schema's own MarshalJSON.
func canonicalizeJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("agentsdk: canonicalize: %w", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("agentsdk: canonicalize: %w", err)
	}
	return out, nil
}

// TelemetrySchemaAnnouncement is the payload an agent publishes (retained) to
// its telemetry-schema topic. It is advisory — see DeriveTelemetrySchema.
type TelemetrySchemaAnnouncement struct {
	AgentID    string          `json:"agent_id"`
	Timestamp  time.Time       `json:"timestamp"`
	SchemaHash string          `json:"schema_hash"`
	Schema     json.RawMessage `json:"schema"`
}
