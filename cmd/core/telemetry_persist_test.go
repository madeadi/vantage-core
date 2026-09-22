package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"vantageos-core/cmd/core/telemetry/events"
	"vantageos-core/cmd/core/telemetry/ingest"
	"vantageos-core/cmd/core/telemetry/mapping"
	"vantageos-core/cmd/core/telemetry/persistcfg"
	"vantageos-core/cmd/core/telemetry/registry"
	"vantageos-core/cmd/core/telemetry/store"
	"vantageos-core/cmd/core/telemetry/validate"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pocketbase/pocketbase/core"
)

// telemetryTestPool mirrors cmd/core/telemetry/store's own testPool, but
// lives here (package main) since this test also needs a real PocketBase
// instance -- store's test DSN helper is unexported to that package. Skips
// (not fails) when TELEMETRY_TEST_DSN is unset, same reasoning as
// cmd/core/telemetry/store/migrate_test.go.
func telemetryTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TELEMETRY_TEST_DSN")
	if dsn == "" {
		t.Skip("TELEMETRY_TEST_DSN not set -- skipping persistence wiring test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("cannot reach test postgres: %v", err)
	}
	// A plain (non-hypertable) fixture with the same columns as
	// migrations/0001_init.sql -- see that file's doc comment on why this
	// sandbox can't run the real TimescaleDB-specific migration.
	if _, err := pool.Exec(context.Background(), `DROP TABLE IF EXISTS telemetry`); err != nil {
		t.Fatalf("drop telemetry: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		CREATE TABLE telemetry (
			time        timestamptz NOT NULL,
			received_at timestamptz NOT NULL,
			agent_id    text        NOT NULL,
			group_id    text,
			schema_hash text,
			valid       boolean,
			fields      jsonb,
			payload     jsonb       NOT NULL
		)
	`); err != nil {
		t.Fatalf("create telemetry: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS telemetry`) })
	return pool
}

func mustCreateTelemetrySettings(t *testing.T, app core.App, groupRecordID string, enabled bool) {
	t.Helper()
	coll, err := app.FindCollectionByNameOrId(collTelemetrySettings)
	if err != nil {
		t.Fatalf("find telemetry_settings: %v", err)
	}
	r := core.NewRecord(coll)
	r.Set("agent_group", groupRecordID)
	r.Set("persist_enabled", enabled)
	if err := app.Save(r); err != nil {
		t.Fatalf("save telemetry_settings: %v", err)
	}
}

// TestPersistenceWiringEndToEnd is Step 12's wiring-level "Done when":
// toggling persistence off (here, per-group via telemetry_settings) stops
// writes without disabling validation. Exercises the real loaders
// (loadAgentGroupMappings, loadTelemetryPersistSettings) against a real
// PocketBase instance, real Postgres, and enqueueTelemetryRow itself -- the
// same path cmd/core/telemetry.go's ingest listener takes in production,
// just without the mqtt transport in between (Step 6/9's tests already
// cover that leg live).
func TestPersistenceWiringEndToEnd(t *testing.T) {
	app := bootstrapTestApp(t)
	pool := telemetryTestPool(t)

	enabledGroup := mustCreateGroup(t, app, "Warehouse Fleet", batteryTestSchema)
	fresh, err := app.FindRecordById(collAgentGroups, enabledGroup.Id)
	if err != nil {
		t.Fatalf("find group: %v", err)
	}
	mappingJSON, err := json.Marshal(mapping.Spec{Fields: map[string]string{"battery": "battery_percent"}})
	if err != nil {
		t.Fatalf("marshal mapping spec: %v", err)
	}
	fresh.Set("telemetry_mapping", string(mappingJSON))
	if err := app.Save(fresh); err != nil {
		t.Fatalf("save group mapping: %v", err)
	}
	mustCreateAgent(t, app, "persist-test-agent", enabledGroup.Id)
	mustCreateTelemetrySettings(t, app, enabledGroup.Id, true)

	disabledGroup := mustCreateGroup(t, app, "No Persist Fleet", batteryTestSchema)
	mustCreateAgent(t, app, "no-persist-agent", disabledGroup.Id)
	// No telemetry_settings row for disabledGroup at all -- the migration's
	// documented default (no row = disabled), not an explicit false.

	schemas, err := loadAgentGroupSchemas(app)
	if err != nil {
		t.Fatalf("loadAgentGroupSchemas: %v", err)
	}
	memberships, err := loadAgentGroupMemberships(app)
	if err != nil {
		t.Fatalf("loadAgentGroupMemberships: %v", err)
	}
	schemaRegistry := registry.New()
	schemaRegistry.SetAgentGroups(memberships)
	if err := schemaRegistry.SetGroupSchemas(schemas); err != nil {
		t.Fatalf("SetGroupSchemas: %v", err)
	}

	mappings, err := loadAgentGroupMappings(app)
	if err != nil {
		t.Fatalf("loadAgentGroupMappings: %v", err)
	}
	settings, err := loadTelemetryPersistSettings(app)
	if err != nil {
		t.Fatalf("loadTelemetryPersistSettings: %v", err)
	}
	persistRegistry := persistcfg.New()
	persistRegistry.SetMappings(mappings)
	persistRegistry.SetPersistEnabled(settings)

	persistStore := store.New(pool, store.Config{BatchSize: 10, FlushInterval: 20 * time.Millisecond})
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go persistStore.Run(runCtx)

	throttler := events.New(events.NewPocketBaseSink(app), events.Config{})
	validator := validate.New(schemaRegistry)

	goodPayload := []byte(`{"battery_percent": 42}`)
	goodViolation := validator.Validate("persist-test-agent", goodPayload)
	if goodViolation != nil {
		t.Fatalf("good payload should validate cleanly, got: %+v", goodViolation)
	}
	enqueueTelemetryRow(persistStore, persistRegistry, schemaRegistry, throttler, ingest.Message{
		AgentID: "persist-test-agent", Kind: ingest.KindTelemetry, Payload: goodPayload, ReceivedAt: time.Now().UTC(),
	}, goodViolation)

	// This group has no telemetry_settings row -- persistence must stay
	// disabled for it even though the payload is perfectly valid (Step 12's
	// "stops writes without disabling validation": validation still ran
	// above the persistence gate, it just doesn't get written).
	disabledPayload := []byte(`{"battery_percent": 10}`)
	disabledViolation := validator.Validate("no-persist-agent", disabledPayload)
	if disabledViolation != nil {
		t.Fatalf("disabled-group payload should also validate cleanly, got: %+v", disabledViolation)
	}
	enqueueTelemetryRow(persistStore, persistRegistry, schemaRegistry, throttler, ingest.Message{
		AgentID: "no-persist-agent", Kind: ingest.KindTelemetry, Payload: disabledPayload, ReceivedAt: time.Now().UTC(),
	}, disabledViolation)

	deadline := time.After(2 * time.Second)
	for persistStore.Stats().Written < 1 {
		select {
		case <-deadline:
			t.Fatal("no row was ever written")
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond) // let a second, wrongly-written row (if any) land before counting

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM telemetry`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("telemetry has %d rows, want exactly 1 -- the disabled group's telemetry must not have been written", count)
	}

	var agentID, groupID, schemaHash string
	var valid bool
	var fields []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT agent_id, group_id, schema_hash, valid, fields FROM telemetry LIMIT 1`,
	).Scan(&agentID, &groupID, &schemaHash, &valid, &fields); err != nil {
		t.Fatalf("query written row: %v", err)
	}
	if agentID != "persist-test-agent" {
		t.Errorf("agent_id = %q, want persist-test-agent", agentID)
	}
	if groupID != enabledGroup.Id {
		t.Errorf("group_id = %q, want %q", groupID, enabledGroup.Id)
	}
	if schemaHash == "" {
		t.Error("schema_hash is empty, want the enabled group's compiled contract hash")
	}
	if !valid {
		t.Error("valid = false, want true -- the payload validated cleanly")
	}
	if string(fields) != `{"battery": 42}` && string(fields) != `{"battery":42}` {
		t.Errorf("fields = %s, want the mapped battery=42 projection", fields)
	}
}
