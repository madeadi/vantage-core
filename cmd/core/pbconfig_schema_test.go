package main

import (
	"testing"

	"vantageos-core/cmd/core/telemetry/registry"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

const batteryTestSchema = `{"type":"object","properties":{"battery_percent":{"type":"number"}},"required":["battery_percent"]}`
const poseTestSchema = `{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"]}`

// bootstrapTestApp starts a real embedded PocketBase in a fresh temp dir,
// running every migration in cmd/core/migrations (imported for side effects
// in main.go) -- the same path a production core instance takes, not a
// hand-rolled schema. Step 10's migrations, including the ones this test
// exercises (agent_groups.telemetry_schema, agents.agent_group), run for real.
func bootstrapTestApp(t *testing.T) *pocketbase.PocketBase {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err := app.RunAppMigrations(); err != nil {
		t.Fatalf("RunAppMigrations: %v", err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	return app
}

func mustCreateGroup(t *testing.T, app core.App, name, schema string) *core.Record {
	t.Helper()
	coll, err := app.FindCollectionByNameOrId(collAgentGroups)
	if err != nil {
		t.Fatalf("find agent_groups: %v", err)
	}
	r := core.NewRecord(coll)
	r.Set("name", name)
	if schema != "" {
		r.Set("telemetry_schema", schema)
	}
	if err := app.Save(r); err != nil {
		t.Fatalf("save group: %v", err)
	}
	return r
}

func mustCreateAgent(t *testing.T, app core.App, agentID, groupRecordID string) *core.Record {
	t.Helper()
	coll, err := app.FindCollectionByNameOrId(collAgents)
	if err != nil {
		t.Fatalf("find agents: %v", err)
	}
	r := core.NewRecord(coll)
	r.Set("agent_id", agentID)
	r.Set("key", "test-key-"+agentID)
	if groupRecordID != "" {
		r.Set("agent_group", groupRecordID)
	}
	if err := app.Save(r); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	return r
}

// TestSchemaRegistryLoadersAgainstRealPocketBase is Step 7's end-to-end
// proof: real migrations, real records, real JSONField/RelationField
// extraction (both loadAgentGroupSchemas and loadAgentGroupMemberships --
// see their doc comments for why the exact Go types PocketBase hands back
// were worth verifying directly rather than assuming), feeding a real
// registry.Registry.
func TestSchemaRegistryLoadersAgainstRealPocketBase(t *testing.T) {
	app := bootstrapTestApp(t)

	group := mustCreateGroup(t, app, "Warehouse Fleet", batteryTestSchema)
	mustCreateAgent(t, app, "schema-test-agent", group.Id)
	mustCreateAgent(t, app, "no-group-bot", "") // never assigned a group

	schemas, err := loadAgentGroupSchemas(app)
	if err != nil {
		t.Fatalf("loadAgentGroupSchemas: %v", err)
	}
	memberships, err := loadAgentGroupMemberships(app)
	if err != nil {
		t.Fatalf("loadAgentGroupMemberships: %v", err)
	}

	reg := registry.New()
	reg.SetAgentGroups(memberships)
	if err := reg.SetGroupSchemas(schemas); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}

	entry, ok := reg.Resolve("schema-test-agent")
	if !ok {
		t.Fatal("Resolve(schema-test-agent): want ok=true")
	}
	if entry.GroupID != group.Id {
		t.Errorf("GroupID = %q, want %q", entry.GroupID, group.Id)
	}
	if err := entry.Resolved.Validate(map[string]any{"battery_percent": 50.0}); err != nil {
		t.Errorf("Validate(valid telemetry): %v", err)
	}
	if err := entry.Resolved.Validate(map[string]any{"status": "idle"}); err == nil {
		t.Error("Validate(missing required field): want error, got nil")
	}

	if _, ok := reg.Resolve("no-group-bot"); ok {
		t.Error("Resolve(no-group-bot): want ok=false (no_contract) for an agent with no group")
	}
	if _, ok := reg.Resolve("never-heard-of-it"); ok {
		t.Error("Resolve(unknown agent): want ok=false")
	}
}

// TestSchemaRegistryReloadReflectsLiveGroupEdit is the literal Step 7 "Done
// when": editing a group's schema takes effect with no restart. It re-runs
// the loaders the same way the watchConfig hook does on every
// OnRecordAfter*Success event -- see cmd/core/pbconfig.go's
// reloadAgentGroupSchemas -- rather than re-testing that PocketBase's own
// hook machinery fires (already relied on, unmodified, by every other
// collection watchConfig binds).
func TestSchemaRegistryReloadReflectsLiveGroupEdit(t *testing.T) {
	app := bootstrapTestApp(t)

	group := mustCreateGroup(t, app, "Warehouse Fleet", batteryTestSchema)
	mustCreateAgent(t, app, "schema-test-agent", group.Id)

	reg := registry.New()
	reload := func() {
		schemas, err := loadAgentGroupSchemas(app)
		if err != nil {
			t.Fatalf("loadAgentGroupSchemas: %v", err)
		}
		memberships, err := loadAgentGroupMemberships(app)
		if err != nil {
			t.Fatalf("loadAgentGroupMemberships: %v", err)
		}
		reg.SetAgentGroups(memberships)
		if err := reg.SetGroupSchemas(schemas); err != nil {
			t.Fatalf("SetGroupSchemas: %v", err)
		}
	}
	reload()

	before, ok := reg.Resolve("schema-test-agent")
	if !ok {
		t.Fatal("Resolve before edit: want ok=true")
	}
	if err := before.Resolved.Validate(map[string]any{"x": 1.0, "y": 2.0}); err == nil {
		t.Fatal("pose-shaped payload validated against the battery schema before the edit -- test setup is wrong")
	}

	// The live edit: an admin changes the group's contract, same as the
	// PocketBase admin UI would (app.Save on an existing record fires the
	// OnRecordAfterUpdateSuccess hook watchConfig binds reloadAgentGroupSchemas to).
	fresh, err := app.FindRecordById(collAgentGroups, group.Id)
	if err != nil {
		t.Fatalf("find group: %v", err)
	}
	fresh.Set("telemetry_schema", poseTestSchema)
	if err := app.Save(fresh); err != nil {
		t.Fatalf("save edited group: %v", err)
	}

	reload() // what the bound hook does

	after, ok := reg.Resolve("schema-test-agent")
	if !ok {
		t.Fatal("Resolve after edit: want ok=true")
	}
	if after.Hash == before.Hash {
		t.Error("hash unchanged after editing the group's schema -- edit did not take effect")
	}
	if err := after.Resolved.Validate(map[string]any{"x": 1.0, "y": 2.0}); err != nil {
		t.Errorf("Validate(pose payload) after switching to the pose schema: %v", err)
	}
	if err := after.Resolved.Validate(map[string]any{"battery_percent": 50.0}); err == nil {
		t.Error("Validate(old battery payload) against the new pose schema: want error, got nil -- registry still serving the stale schema")
	}
}

// TestSchemaRegistryReloadReflectsAgentReassignment covers the other half
// of Step 7's live-reload requirement: reassigning which group an agent
// belongs to (an agents.agent_group edit, not an agent_groups edit) must
// also take effect immediately.
func TestSchemaRegistryReloadReflectsAgentReassignment(t *testing.T) {
	app := bootstrapTestApp(t)

	groupA := mustCreateGroup(t, app, "Fleet A", batteryTestSchema)
	groupB := mustCreateGroup(t, app, "Fleet B", poseTestSchema)
	agent := mustCreateAgent(t, app, "schema-test-agent", groupA.Id)

	reg := registry.New()
	reload := func() {
		schemas, err := loadAgentGroupSchemas(app)
		if err != nil {
			t.Fatalf("loadAgentGroupSchemas: %v", err)
		}
		memberships, err := loadAgentGroupMemberships(app)
		if err != nil {
			t.Fatalf("loadAgentGroupMemberships: %v", err)
		}
		reg.SetAgentGroups(memberships)
		if err := reg.SetGroupSchemas(schemas); err != nil {
			t.Fatalf("SetGroupSchemas: %v", err)
		}
	}
	reload()

	before, _ := reg.Resolve("schema-test-agent")
	if before.GroupID != groupA.Id {
		t.Fatalf("GroupID before reassignment = %q, want %q", before.GroupID, groupA.Id)
	}

	fresh, err := app.FindRecordById(collAgents, agent.Id)
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}
	fresh.Set("agent_group", groupB.Id)
	if err := app.Save(fresh); err != nil {
		t.Fatalf("save reassigned agent: %v", err)
	}

	reload()

	after, ok := reg.Resolve("schema-test-agent")
	if !ok {
		t.Fatal("Resolve after reassignment: want ok=true")
	}
	if after.GroupID != groupB.Id {
		t.Errorf("GroupID after reassignment = %q, want %q", after.GroupID, groupB.Id)
	}
	if err := after.Resolved.Validate(map[string]any{"x": 1.0, "y": 2.0}); err != nil {
		t.Errorf("Validate against the new group's schema: %v", err)
	}
}
