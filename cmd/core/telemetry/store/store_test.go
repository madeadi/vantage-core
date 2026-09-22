package store

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// setupTelemetryTable creates the telemetry table's columns exactly as
// migrations/0001_init.sql defines them, minus the TimescaleDB-specific
// statements (CREATE EXTENSION, create_hypertable, compression, retention)
// -- those require a real TimescaleDB-enabled Postgres server, which this
// sandbox's plain PostgreSQL 16 install does not have (confirmed: no
// timescaledb package installed, and Docker's daemon isn't running here
// either). A hypertable is, from an INSERT/SELECT client's perspective, the
// same table with transparent chunking underneath -- everything this
// package's own logic does (batching, retries, nullable columns, jsonb
// encoding) is identical either way. What this fixture does NOT verify is
// that migrations/0001_init.sql's TimescaleDB-specific statements
// themselves succeed against a real TimescaleDB server; that needs manual
// or CI verification against real TimescaleDB, which this sandbox cannot do.
func setupTelemetryTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS telemetry`); err != nil {
		t.Fatalf("drop telemetry table: %v", err)
	}
	if _, err := pool.Exec(ctx, `
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
		t.Fatalf("create telemetry table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS telemetry`)
	})
}

func boolPtr(b bool) *bool { return &b }

func TestStoreEnqueueAndRunWritesRows(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	s := New(pool, Config{BatchSize: 100, FlushInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	now := time.Now().UTC().Truncate(time.Millisecond)
	s.Enqueue(Row{
		Time: now, ReceivedAt: now, AgentID: "smallbot", GroupID: "group1", SchemaHash: "hash1",
		Valid: boolPtr(true), Fields: json.RawMessage(`{"x":1}`), Payload: json.RawMessage(`{"x":1,"status":"idle"}`),
	})

	waitForRowCount(t, pool, 1, time.Second)

	var agentID, groupID, schemaHash string
	var valid bool
	var fields, payload []byte
	err := pool.QueryRow(context.Background(),
		`SELECT agent_id, group_id, schema_hash, valid, fields, payload FROM telemetry LIMIT 1`,
	).Scan(&agentID, &groupID, &schemaHash, &valid, &fields, &payload)
	if err != nil {
		t.Fatalf("query written row: %v", err)
	}
	if agentID != "smallbot" || groupID != "group1" || schemaHash != "hash1" || !valid {
		t.Errorf("got agent_id=%q group_id=%q schema_hash=%q valid=%v", agentID, groupID, schemaHash, valid)
	}
	if string(fields) != `{"x": 1}` && string(fields) != `{"x":1}` {
		t.Errorf("fields = %s, want {\"x\":1}", fields)
	}

	if got := s.Stats().Written; got != 1 {
		t.Errorf("Stats().Written = %d, want 1", got)
	}
}

func TestStoreNullableColumnsForNoContractRow(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	s := New(pool, Config{BatchSize: 100, FlushInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	now := time.Now().UTC()
	// no_contract: GroupID, SchemaHash empty, Valid nil, Fields nil -- must
	// all land as SQL NULL, not empty-string/false placeholders.
	s.Enqueue(Row{
		Time: now, ReceivedAt: now, AgentID: "ungrouped-bot",
		Payload: json.RawMessage(`{"whatever":true}`),
	})

	waitForRowCount(t, pool, 1, time.Second)

	var groupID, schemaHash *string
	var valid *bool
	var fields *[]byte
	err := pool.QueryRow(context.Background(),
		`SELECT group_id, schema_hash, valid, fields FROM telemetry LIMIT 1`,
	).Scan(&groupID, &schemaHash, &valid, &fields)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if groupID != nil {
		t.Errorf("group_id = %v, want NULL", *groupID)
	}
	if schemaHash != nil {
		t.Errorf("schema_hash = %v, want NULL", *schemaHash)
	}
	if valid != nil {
		t.Errorf("valid = %v, want NULL", *valid)
	}
	if fields != nil {
		t.Errorf("fields = %s, want NULL", *fields)
	}
}

func TestStoreBatchesBySize(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	// Long flush interval -- the only thing that should trigger a flush
	// here is reaching BatchSize.
	s := New(pool, Config{BatchSize: 5, FlushInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		s.Enqueue(Row{Time: now, ReceivedAt: now, AgentID: "smallbot", Payload: json.RawMessage(`{}`)})
	}

	waitForRowCount(t, pool, 5, time.Second)
}

func TestStoreFlushesOnIntervalBelowBatchSize(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	s := New(pool, Config{BatchSize: 1000, FlushInterval: 30 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	now := time.Now().UTC()
	s.Enqueue(Row{Time: now, ReceivedAt: now, AgentID: "smallbot", Payload: json.RawMessage(`{}`)})
	// Only 1 row, batch size is 1000 -- only the flush interval can write it.

	waitForRowCount(t, pool, 1, time.Second)
}

func TestStoreEnqueueDropsAndCountsWhenQueueFull(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	// Tiny queue, Run never started -- nothing drains it, so it fills
	// immediately.
	s := New(pool, Config{QueueSize: 2})

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		s.Enqueue(Row{Time: now, ReceivedAt: now, AgentID: "smallbot", Payload: json.RawMessage(`{}`)})
	}

	if got := s.Stats().Dropped; got != 3 {
		t.Errorf("Stats().Dropped = %d, want 3 (5 enqueued - 2 capacity)", got)
	}
}

func TestStoreContextCancelFlushesRemainingRows(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	s := New(pool, Config{BatchSize: 1000, FlushInterval: time.Hour}) // neither trigger fires on its own
	ctx, cancel := context.WithCancel(context.Background())

	now := time.Now().UTC()
	s.Enqueue(Row{Time: now, ReceivedAt: now, AgentID: "smallbot", Payload: json.RawMessage(`{}`)})

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond) // let Run pick the row off the queue
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of context cancellation")
	}

	waitForRowCount(t, pool, 1, time.Second)
}

// TestStoreKillingPostgresMidRunDropsRowsWithACountRatherThanDeadlocking is
// spec Step 12's literal "Done when": killing Postgres mid-run must not
// deadlock ingest. This test actually stops the Postgres service (not a
// simulation) mid-Run, confirms Enqueue keeps accepting and Run keeps
// making progress (never blocks) while writes fail, and confirms the
// dropped rows are counted via Stats().Failed once retries are exhausted --
// then restarts Postgres for every later test in this package.
func TestStoreKillingPostgresMidRunDropsRowsWithACountRatherThanDeadlocking(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	s := New(pool, Config{
		BatchSize: 1, FlushInterval: 10 * time.Millisecond,
		MaxRetries: 2, RetryBackoff: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	now := time.Now().UTC()
	s.Enqueue(Row{Time: now, ReceivedAt: now, AgentID: "before-outage", Payload: json.RawMessage(`{}`)})
	waitForRowCount(t, pool, 1, time.Second)

	if out, err := exec.Command("service", "postgresql", "stop").CombinedOutput(); err != nil {
		t.Fatalf("stop postgresql: %v: %s", err, out)
	}
	stopped := true
	defer func() {
		if stopped {
			if out, err := exec.Command("service", "postgresql", "start").CombinedOutput(); err != nil {
				t.Fatalf("restart postgresql: %v: %s", err, out)
			}
			waitForPostgresUp(t, pool, 5*time.Second)
		}
	}()

	// Enqueue must keep accepting rows without blocking, even though every
	// write from here is about to fail.
	enqueued := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			s.Enqueue(Row{Time: now, ReceivedAt: now, AgentID: "during-outage", Payload: json.RawMessage(`{}`)})
		}
		close(enqueued)
	}()
	select {
	case <-enqueued:
	case <-time.After(time.Second):
		t.Fatal("Enqueue blocked while Postgres was down -- it must never block the caller")
	}

	// With Postgres down, writeBatch must exhaust its retries and count the
	// row as Failed, not hang Run indefinitely.
	deadline := time.After(3 * time.Second)
	for {
		if s.Stats().Failed > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Stats().Failed still 0 after 3s with Postgres down -- Run appears stuck instead of dropping and counting")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if out, err := exec.Command("service", "postgresql", "start").CombinedOutput(); err != nil {
		t.Fatalf("restart postgresql: %v: %s", err, out)
	}
	stopped = false
	waitForPostgresUp(t, pool, 5*time.Second)

	// Run itself must still be alive after the outage, not wedged.
	now2 := time.Now().UTC()
	s.Enqueue(Row{Time: now2, ReceivedAt: now2, AgentID: "after-outage", Payload: json.RawMessage(`{}`)})
	waitForRowCount(t, pool, 2, 2*time.Second) // before-outage + after-outage; during-outage rows were dropped

	cancel()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation post-outage")
	}
}

func waitForRowCount(t *testing.T, pool *pgxpool.Pool, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		var count int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM telemetry`).Scan(&count); err == nil && count >= want {
			if count > want {
				t.Fatalf("telemetry has %d rows, want exactly %d", count, want)
			}
			return
		}
		select {
		case <-deadline:
			var count int
			_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM telemetry`).Scan(&count)
			t.Fatalf("telemetry has %d rows after %v, want %d", count, timeout, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitForPostgresUp(t *testing.T, pool *pgxpool.Pool, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if err := pool.Ping(context.Background()); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("Postgres did not come back up within %v", timeout)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
