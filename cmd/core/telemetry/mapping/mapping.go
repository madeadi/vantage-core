// Package mapping resolves a group's agent_groups.telemetry_mapping into
// concrete agent_id/timestamp/fields values for one ingested telemetry
// payload, ready for Step 12's persistence layer. See
// specs/mqtt_telemetry.specs.md Step 11.
package mapping

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vantageos-core/cmd/core/telemetry/validate"
)

// saneWindow bounds how far a mapped timestamp may sit from receive time
// before it's rejected as bad_timestamp rather than written to the
// hypertable -- see specs/mqtt_telemetry.specs.md Step 11.
const saneWindow = 24 * time.Hour

// Spec is agent_groups.telemetry_mapping, decoded. All fields are optional;
// see Apply's doc comment for the defaults an empty/nil Spec falls back to.
type Spec struct {
	// AgentID is a dotted path (an optional leading "$." is stripped) into
	// the telemetry payload naming which field carries the agent's own
	// identifier, e.g. "$.robot.serial".
	AgentID string `json:"agent_id"`
	// Timestamp is a dotted path naming which field carries the event's own
	// timestamp, e.g. "$.header.stamp".
	Timestamp string `json:"timestamp"`
	// TimestampFormat is how to parse the value at Timestamp: "rfc3339",
	// "unix_ms", or "unix_s". Defaults to "rfc3339" when Timestamp is set
	// but this is not.
	TimestampFormat string `json:"timestamp_format"`
	// Fields projects hot values out of the payload into real columns for
	// cheap replay queries -- a name -> dotted-path map. The full payload
	// is still stored regardless (as jsonb); this is a projection, not a
	// replacement.
	Fields map[string]string `json:"fields"`
}

// Result is what Apply resolves for one payload.
type Result struct {
	AgentID    string
	Time       time.Time
	ReceivedAt time.Time
	Fields     map[string]any
}

// Apply resolves a Result for payload under spec.
//
// Defaults when spec is nil, or a given field of it is unset (the common
// case, per specs/mqtt_telemetry.specs.md): AgentID comes from
// topicAgentID (the agent ID the SDK's topic layout already carries --
// reliable regardless of payload mapping, see pkg/agentsdk.AgentIDFromTopic),
// Time comes from receivedAt. ReceivedAt is always set to receivedAt,
// regardless of mapping -- Step 12 stores both, since agent clocks skew and
// replay needs a fallback ordering.
//
// A configured AgentID or Timestamp path that isn't found in the payload
// falls back silently -- topicAgentID and receivedAt are both reliable
// substitutes, so this isn't reported as a violation. A Timestamp path
// that IS found but fails to parse, or resolves outside saneWindow of
// receivedAt, also falls back to receivedAt (never written as-is: see
// saneWindow's doc comment), but is reported via the returned
// *validate.Violation (Kind: KindBadTimestamp) so it still surfaces to the
// same throttled agent_events path every other violation does. AgentID has
// no equivalent violation Kind: the spec's Kind enum doesn't define one,
// since the topic-derived fallback makes a bad AgentID path far less
// consequential than a bad Timestamp path, which has no source of truth
// this reliable to fall back to.
func Apply(spec *Spec, topicAgentID, groupID string, receivedAt time.Time, payload []byte) (Result, *validate.Violation) {
	result := Result{
		AgentID:    topicAgentID,
		Time:       receivedAt,
		ReceivedAt: receivedAt,
	}

	if spec == nil {
		return result, nil
	}

	var instance any
	if err := json.Unmarshal(payload, &instance); err != nil {
		// Not this layer's job to report malformed JSON -- validate.Validate
		// already does (KindNotJSON). Just fall back to defaults.
		return result, nil
	}

	if spec.AgentID != "" {
		if v, ok := extract(instance, spec.AgentID); ok {
			if s, ok := v.(string); ok && s != "" {
				result.AgentID = s
			}
		}
	}

	var violation *validate.Violation
	if spec.Timestamp != "" {
		if v, ok := extract(instance, spec.Timestamp); ok {
			t, parseErr := parseTimestamp(v, spec.TimestampFormat)
			switch {
			case parseErr != nil:
				violation = &validate.Violation{
					AgentID: result.AgentID, GroupID: groupID,
					Kind:   validate.KindBadTimestamp,
					Paths:  []string{normalizePath(spec.Timestamp)},
					Detail: parseErr.Error(),
				}
			case !withinSaneWindow(t, receivedAt):
				violation = &validate.Violation{
					AgentID: result.AgentID, GroupID: groupID,
					Kind:  validate.KindBadTimestamp,
					Paths: []string{normalizePath(spec.Timestamp)},
					Detail: fmt.Sprintf("mapped timestamp %s is more than %s from receive time %s",
						t.Format(time.RFC3339), saneWindow, receivedAt.Format(time.RFC3339)),
				}
			default:
				result.Time = t
			}
		}
		// Path configured but absent from this payload: silent fallback,
		// same reasoning as AgentID above.
	}

	if len(spec.Fields) > 0 {
		fields := make(map[string]any, len(spec.Fields))
		for name, path := range spec.Fields {
			if v, ok := extract(instance, path); ok {
				fields[name] = v
			}
		}
		result.Fields = fields
	}

	return result, violation
}

// normalizePath strips an optional leading "$." or "$" so a Violation's
// Paths (JSON Pointers elsewhere in this package's siblings) reads as
// "/header/stamp" rather than mixing dot and pointer syntax.
func normalizePath(path string) string {
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	return "/" + strings.ReplaceAll(path, ".", "/")
}

// extract walks payload (expected to be nested map[string]any, the shape
// encoding/json produces for a JSON object) along path's dot-separated
// segments, returning the value at that location. An optional leading "$."
// or "$" is stripped first. Only object nesting is supported -- array
// indexing is not, matching every example in
// specs/mqtt_telemetry.specs.md's mapping spec.
func extract(payload any, path string) (any, bool) {
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	if path == "" {
		return payload, true
	}

	cur := payload
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[seg]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// parseTimestamp parses v (the value extract found) as a time.Time per
// format ("rfc3339", "unix_ms", "unix_s", or "" defaulting to "rfc3339").
func parseTimestamp(v any, format string) (time.Time, error) {
	switch format {
	case "", "rfc3339":
		s, ok := v.(string)
		if !ok {
			return time.Time{}, fmt.Errorf("timestamp value %v is not a string, want rfc3339", v)
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, fmt.Errorf("timestamp %q is not valid rfc3339: %w", s, err)
		}
		return t, nil

	case "unix_ms":
		f, ok := toNumber(v)
		if !ok {
			return time.Time{}, fmt.Errorf("timestamp value %v is not a number, want unix_ms", v)
		}
		return time.UnixMilli(int64(f)).UTC(), nil

	case "unix_s":
		f, ok := toNumber(v)
		if !ok {
			return time.Time{}, fmt.Errorf("timestamp value %v is not a number, want unix_s", v)
		}
		return time.Unix(int64(f), 0).UTC(), nil

	default:
		return time.Time{}, fmt.Errorf("unknown timestamp_format %q, want rfc3339, unix_ms, or unix_s", format)
	}
}

// toNumber returns v as a float64 if it's a JSON number -- encoding/json
// always unmarshals a JSON number into float64 when the destination is any.
func toNumber(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func withinSaneWindow(t, receivedAt time.Time) bool {
	diff := t.Sub(receivedAt)
	if diff < 0 {
		diff = -diff
	}
	return diff <= saneWindow
}
