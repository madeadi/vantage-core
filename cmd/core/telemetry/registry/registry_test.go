package registry

import (
	"encoding/json"
	"testing"
)

const batterySchema = `{"type":"object","properties":{"battery_percent":{"type":"number"}},"required":["battery_percent"]}`
const poseSchema = `{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"]}`

func TestResolveNoGroup(t *testing.T) {
	r := New()
	r.SetAgentGroups(map[string]string{}) // "smallbot" has no group at all
	if err := r.SetGroupSchemas(map[string]json.RawMessage{}); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}

	if _, ok := r.Resolve("smallbot"); ok {
		t.Error("Resolve for an agent with no group: want ok=false")
	}
}

func TestResolveGroupWithNoSchema(t *testing.T) {
	r := New()
	r.SetAgentGroups(map[string]string{"smallbot": "group1"})
	// group1 exists but has no telemetry_schema set -- absent from the map,
	// matching how a PocketBase JSON field with no value would be loaded.
	if err := r.SetGroupSchemas(map[string]json.RawMessage{}); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}

	if _, ok := r.Resolve("smallbot"); ok {
		t.Error("Resolve for a group with no schema set: want ok=false (no_contract)")
	}
}

func TestResolveGroupWithNullSchema(t *testing.T) {
	r := New()
	r.SetAgentGroups(map[string]string{"smallbot": "group1"})
	if err := r.SetGroupSchemas(map[string]json.RawMessage{"group1": json.RawMessage("null")}); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}

	if _, ok := r.Resolve("smallbot"); ok {
		t.Error("Resolve for a group with an explicit null schema: want ok=false (no_contract)")
	}
}

func TestResolveHappyPath(t *testing.T) {
	r := New()
	r.SetAgentGroups(map[string]string{"smallbot": "group1"})
	if err := r.SetGroupSchemas(map[string]json.RawMessage{"group1": json.RawMessage(batterySchema)}); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}

	entry, ok := r.Resolve("smallbot")
	if !ok {
		t.Fatal("Resolve: want ok=true")
	}
	if entry.GroupID != "group1" {
		t.Errorf("GroupID = %q, want %q", entry.GroupID, "group1")
	}
	if entry.Resolved == nil {
		t.Fatal("Resolved is nil")
	}

	if err := entry.Resolved.Validate(map[string]any{"battery_percent": 55.0}); err != nil {
		t.Errorf("Validate(valid instance): %v", err)
	}
	if err := entry.Resolved.Validate(map[string]any{"status": "idle"}); err == nil {
		t.Error("Validate(missing required field): want error, got nil")
	}
}

func TestCompileOncePerDistinctHash(t *testing.T) {
	r := New()
	r.SetAgentGroups(map[string]string{"a": "group1", "b": "group2"})
	// Two different groups, byte-identical schema -- must compile once and
	// share the compiled entry, not compile twice.
	if err := r.SetGroupSchemas(map[string]json.RawMessage{
		"group1": json.RawMessage(batterySchema),
		"group2": json.RawMessage(batterySchema),
	}); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}

	entryA, okA := r.Resolve("a")
	entryB, okB := r.Resolve("b")
	if !okA || !okB {
		t.Fatalf("Resolve: okA=%v okB=%v, want both true", okA, okB)
	}
	if entryA.Hash != entryB.Hash {
		t.Fatalf("hashes differ for byte-identical schemas: %q vs %q", entryA.Hash, entryB.Hash)
	}
	if entryA.Resolved != entryB.Resolved {
		t.Error("two groups with identical schemas got different *Resolved instances, want the same cached one")
	}

	r.mu.RLock()
	compiledCount := len(r.compiled)
	r.mu.RUnlock()
	if compiledCount != 1 {
		t.Errorf("compiled cache has %d entries, want 1 (shared by hash)", compiledCount)
	}
}

func TestReloadRecompilesOnlyChangedSchema(t *testing.T) {
	r := New()
	r.SetAgentGroups(map[string]string{"a": "group1"})
	if err := r.SetGroupSchemas(map[string]json.RawMessage{"group1": json.RawMessage(batterySchema)}); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}
	first, _ := r.Resolve("a")

	// Reload with the exact same schema (e.g. an unrelated field on the
	// group record changed) -- must be a cache hit: same *Resolved pointer.
	if err := r.SetGroupSchemas(map[string]json.RawMessage{"group1": json.RawMessage(batterySchema)}); err != nil {
		t.Fatalf("SetGroupSchemas (reload, unchanged): %v", err)
	}
	second, _ := r.Resolve("a")
	if first.Resolved != second.Resolved {
		t.Error("reloading with an unchanged schema recompiled instead of hitting the cache")
	}

	// Now actually change the schema -- must recompile and the hash must
	// change, proving the edit took effect with no restart.
	if err := r.SetGroupSchemas(map[string]json.RawMessage{"group1": json.RawMessage(poseSchema)}); err != nil {
		t.Fatalf("SetGroupSchemas (reload, changed): %v", err)
	}
	third, ok := r.Resolve("a")
	if !ok {
		t.Fatal("Resolve after schema change: want ok=true")
	}
	if third.Hash == second.Hash {
		t.Error("hash did not change after the group's schema actually changed")
	}
	if err := third.Resolved.Validate(map[string]any{"x": 1.0, "y": 2.0}); err != nil {
		t.Errorf("Validate against the new schema: %v", err)
	}
}

func TestReloadEvictsUnreferencedSchema(t *testing.T) {
	r := New()
	r.SetAgentGroups(map[string]string{"a": "group1"})
	if err := r.SetGroupSchemas(map[string]json.RawMessage{"group1": json.RawMessage(batterySchema)}); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}
	r.mu.RLock()
	before := len(r.compiled)
	r.mu.RUnlock()
	if before != 1 {
		t.Fatalf("compiled cache has %d entries after first load, want 1", before)
	}

	// group1 is deleted / no group points at that hash any more.
	if err := r.SetGroupSchemas(map[string]json.RawMessage{}); err != nil {
		t.Fatalf("SetGroupSchemas (empty): %v", err)
	}
	r.mu.RLock()
	after := len(r.compiled)
	r.mu.RUnlock()
	if after != 0 {
		t.Errorf("compiled cache has %d entries after the only referencing group was removed, want 0", after)
	}
}

func TestSetGroupSchemasOneMalformedGroupDoesNotPoisonOthers(t *testing.T) {
	r := New()
	r.SetAgentGroups(map[string]string{"a": "good", "b": "bad"})

	err := r.SetGroupSchemas(map[string]json.RawMessage{
		"good": json.RawMessage(batterySchema),
		"bad":  json.RawMessage(`{not valid json`),
	})
	if err == nil {
		t.Fatal("SetGroupSchemas with one malformed group: want a non-nil error")
	}

	if _, ok := r.Resolve("a"); !ok {
		t.Error("the group with a valid schema must still resolve despite another group's malformed schema")
	}
	if _, ok := r.Resolve("b"); ok {
		t.Error("the group with a malformed schema must not resolve")
	}
}

func TestSetAgentGroupsReassignmentTakesEffectImmediately(t *testing.T) {
	r := New()
	r.SetAgentGroups(map[string]string{"a": "group1"})
	if err := r.SetGroupSchemas(map[string]json.RawMessage{
		"group1": json.RawMessage(batterySchema),
		"group2": json.RawMessage(poseSchema),
	}); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}

	entry1, _ := r.Resolve("a")
	if entry1.GroupID != "group1" {
		t.Fatalf("GroupID = %q, want group1", entry1.GroupID)
	}

	// Agent moved to a different group -- e.g. an admin re-assigned it in
	// the UI. No restart, no schema reload -- just membership.
	r.SetAgentGroups(map[string]string{"a": "group2"})

	entry2, ok := r.Resolve("a")
	if !ok {
		t.Fatal("Resolve after reassignment: want ok=true")
	}
	if entry2.GroupID != "group2" {
		t.Errorf("GroupID after reassignment = %q, want group2", entry2.GroupID)
	}
	if err := entry2.Resolved.Validate(map[string]any{"x": 1.0, "y": 2.0}); err != nil {
		t.Errorf("Validate against the new group's schema: %v", err)
	}
}
