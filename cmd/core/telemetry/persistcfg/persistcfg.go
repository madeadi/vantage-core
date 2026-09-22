// Package persistcfg holds the per-group settings Step 12's persistence
// layer needs beyond validation: agent_groups.telemetry_mapping (Step 11)
// and telemetry_settings.persist_enabled (Step 10). See
// specs/mqtt_telemetry.specs.md Step 12.
//
// This is a separate package from cmd/core/telemetry/registry (which
// resolves an agent's group to its compiled validation contract) rather
// than an extension of it, because registry is imported by
// cmd/core/telemetry/validate, which cmd/core/telemetry/mapping imports for
// its Violation type -- registry importing mapping back would cycle.
// Group membership itself still lives in registry.Registry.GroupID; this
// package is keyed by group id, exactly as registry's own group-scoped
// state is.
package persistcfg

import (
	"sync"

	"vantageos-core/cmd/core/telemetry/mapping"
)

// Registry resolves a group id to its persistence settings. Safe for
// concurrent use. Nothing resolves until SetMappings and SetPersistEnabled
// have been called at least once -- see cmd/core/pbconfig.go.
type Registry struct {
	mu sync.RWMutex

	mappings map[string]*mapping.Spec // group_id -> mapping spec (absent = defaults, see mapping.Apply)
	enabled  map[string]bool          // group_id -> persist_enabled (absent = disabled)
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		mappings: make(map[string]*mapping.Spec),
		enabled:  make(map[string]bool),
	}
}

// SetMappings replaces the group_id -> telemetry_mapping map wholesale.
func (r *Registry) SetMappings(m map[string]*mapping.Spec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mappings = m
}

// SetPersistEnabled replaces the group_id -> persist_enabled map wholesale.
// A group missing from the map (no telemetry_settings row) is treated as
// disabled -- see the telemetry_settings migration's doc comment.
func (r *Registry) SetPersistEnabled(m map[string]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = m
}

// Mapping returns groupID's configured telemetry_mapping, or nil if unset --
// mapping.Apply treats a nil *mapping.Spec as "use the documented defaults".
func (r *Registry) Mapping(groupID string) *mapping.Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mappings[groupID]
}

// PersistEnabled reports whether groupID has persistence turned on.
func (r *Registry) PersistEnabled(groupID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enabled[groupID]
}
