// Package registry resolves an agent to its group's compiled telemetry
// contract. See specs/mqtt_telemetry.specs.md Step 7.
package registry

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"

	"vantageos-core/pkg/agentsdk"
)

// Entry is one agent group's compiled telemetry contract.
type Entry struct {
	GroupID  string
	Hash     string
	Raw      json.RawMessage
	Resolved *jsonschema.Resolved
}

type compiledSchema struct {
	raw      json.RawMessage
	resolved *jsonschema.Resolved
}

// Registry resolves an agent to its group's compiled telemetry contract.
// agent_groups.telemetry_schema is the authoritative validation contract
// (see specs/mqtt_telemetry.specs.md "Group schema is authoritative") — an
// agent's own advisory declared schema (agentsdk.DeriveTelemetrySchema)
// never overrides it.
//
// Compiled schemas are cached by content hash (agentsdk.HashSchema — see
// its doc comment for why both sides of a schema comparison must use the
// same hashing function), not by group ID: two groups sharing a
// byte-identical schema share one compiled entry, and reloading a group
// whose schema string didn't actually change is a cache hit, not a
// recompile. Safe for concurrent use.
type Registry struct {
	mu sync.RWMutex

	agentGroup map[string]string          // agent_id -> group_id
	groupHash  map[string]string          // group_id -> schema hash (absent = no schema set)
	compiled   map[string]*compiledSchema // hash -> compiled schema, shared across groups
}

// New returns an empty Registry. Nothing resolves until SetAgentGroups and
// SetGroupSchemas have been called at least once — see cmd/core/pbconfig.go.
func New() *Registry {
	return &Registry{
		agentGroup: make(map[string]string),
		groupHash:  make(map[string]string),
		compiled:   make(map[string]*compiledSchema),
	}
}

// SetAgentGroups replaces the agent_id -> group_id membership map wholesale,
// matching the bulk-reload convention the rest of cmd/core's config
// registries use (see cmd/core/pbconfig.go's SetAllowedAgents/SetLayouts).
func (r *Registry) SetAgentGroups(m map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentGroup = m
}

// SetGroupSchemas replaces the group_id -> raw telemetry_schema map
// wholesale, compiling any schema not already cached by its content hash
// and evicting any cached schema no group points at any more afterward. A
// group missing from schemas, or mapped to an empty/null value, is treated
// as having no contract set (the no_contract case Resolve reports).
//
// Returns an error naming the offending group if any schema fails to parse
// or resolve; the schemas that did compile successfully are still
// installed — one agent group's malformed contract must not take every
// other group's validation down with it.
func (r *Registry) SetGroupSchemas(schemas map[string]json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	newGroupHash := make(map[string]string, len(schemas))
	var firstErr error

	for groupID, raw := range schemas {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}

		hash, canon, err := agentsdk.HashSchema(raw)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("registry: group %q: invalid schema: %w", groupID, err)
			}
			continue
		}

		if _, ok := r.compiled[hash]; !ok {
			resolved, err := compile(canon)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("registry: group %q: %w", groupID, err)
				}
				continue
			}
			r.compiled[hash] = &compiledSchema{raw: canon, resolved: resolved}
		}

		newGroupHash[groupID] = hash
	}

	// Evict anything no group points at any more under the new mapping.
	live := make(map[string]bool, len(newGroupHash))
	for _, hash := range newGroupHash {
		live[hash] = true
	}
	for hash := range r.compiled {
		if !live[hash] {
			delete(r.compiled, hash)
		}
	}

	r.groupHash = newGroupHash
	return firstErr
}

// Resolve returns the compiled contract for agentID, or ok=false if the
// agent has no group, or its group has no schema set — the no_contract case
// from specs/mqtt_telemetry.specs.md.
func (r *Registry) Resolve(agentID string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	groupID, ok := r.agentGroup[agentID]
	if !ok || groupID == "" {
		return Entry{}, false
	}
	hash, ok := r.groupHash[groupID]
	if !ok {
		return Entry{}, false
	}
	cs, ok := r.compiled[hash]
	if !ok {
		// Should not happen: groupHash and compiled are always updated
		// together in SetGroupSchemas. Treat it as no_contract rather than
		// panicking a validation caller over an internal inconsistency.
		return Entry{}, false
	}

	return Entry{GroupID: groupID, Hash: hash, Raw: cs.raw, Resolved: cs.resolved}, true
}

func compile(raw json.RawMessage) (*jsonschema.Resolved, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve schema: %w", err)
	}
	return resolved, nil
}
