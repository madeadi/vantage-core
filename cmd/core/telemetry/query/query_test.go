package query

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool mirrors cmd/core/telemetry/store's own test helper -- see that
// package's migrate_test.go for the reasoning (skip, don't fail, when no
// real Postgres is configured).
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TELEMETRY_TEST_DSN")
	if dsn == "" {
		t.Skip("TELEMETRY_TEST_DSN not set -- skipping tests that need a real Postgres instance")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("cannot reach test postgres: %v", err)
	}
	return pool
}

// setupTelemetryTable creates a plain-Postgres fixture with the same columns
// as migrations/0001_init.sql, minus the TimescaleDB-specific statements --
// see cmd/core/telemetry/store's own fixture (same reasoning: this
// package's own logic, not TimescaleDB's, is what needs proving here; the
// bucketed time_bucket() path requires TimescaleDB and is not exercised by
// this file).
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
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS telemetry`) })
}

func insertRow(t *testing.T, pool *pgxpool.Pool, agentID string, at time.Time, valid *bool, groupID, schemaHash string, payload string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO telemetry (time, received_at, agent_id, group_id, schema_hash, valid, fields, payload)
		 VALUES ($1, $1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, NULL, $6)`,
		at, agentID, groupID, schemaHash, valid, payload,
	)
	if err != nil {
		t.Fatalf("insert fixture row: %v", err)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestQueryReturnsPointsInTimeRangeOrderedAscending(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	insertRow(t, pool, "bot-1", base, boolPtr(true), "group-a", "hash-a", `{"seq":1}`)
	insertRow(t, pool, "bot-1", base.Add(time.Minute), boolPtr(false), "group-a", "hash-a", `{"seq":2}`)
	insertRow(t, pool, "bot-1", base.Add(2*time.Minute), nil, "", "", `{"seq":3}`)
	insertRow(t, pool, "bot-2", base.Add(time.Minute), boolPtr(true), "group-b", "hash-b", `{"seq":99}`) // different agent

	q := New(pool)
	points, next, err := q.Query(context.Background(), "bot-1", base.Add(-time.Second), base.Add(10*time.Minute), "", 0, "")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if next != "" {
		t.Errorf("next_page_token = %q, want empty (all 3 rows fit under DefaultLimit)", next)
	}
	if len(points) != 3 {
		t.Fatalf("got %d points, want 3", len(points))
	}
	if string(points[0].Payload) != `{"seq": 1}` && string(points[0].Payload) != `{"seq":1}` {
		t.Errorf("points[0].Payload = %s, want seq 1 (ascending order)", points[0].Payload)
	}
	if points[1].Valid == nil || *points[1].Valid != false {
		t.Errorf("points[1].Valid = %v, want *false", points[1].Valid)
	}
	if points[2].Valid != nil {
		t.Errorf("points[2].Valid = %v, want nil (no_contract row)", points[2].Valid)
	}
	if points[0].GroupID != "group-a" || points[0].SchemaHash != "hash-a" {
		t.Errorf("points[0] group/hash = %q/%q, want group-a/hash-a", points[0].GroupID, points[0].SchemaHash)
	}
	if points[2].GroupID != "" || points[2].SchemaHash != "" {
		t.Errorf("points[2] group/hash = %q/%q, want empty (no_contract row)", points[2].GroupID, points[2].SchemaHash)
	}
}

func TestQueryAgentIDRequired(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)
	q := New(pool)
	if _, _, err := q.Query(context.Background(), "", time.Now(), time.Now(), "", 0, ""); err == nil {
		t.Fatal("Query with empty agent_id: want an error, got nil")
	}
}

func TestQueryPaginatesWithPageToken(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	for i := 0; i < 5; i++ {
		insertRow(t, pool, "bot-1", base.Add(time.Duration(i)*time.Minute), boolPtr(true), "g", "h", `{}`)
	}

	q := New(pool)
	from, to := base.Add(-time.Second), base.Add(time.Hour)

	page1, next1, err := q.Query(context.Background(), "bot-1", from, to, "", 2, "")
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1 has %d points, want 2", len(page1))
	}
	if next1 == "" {
		t.Fatal("page 1: want a non-empty next_page_token (3 rows remain)")
	}

	page2, next2, err := q.Query(context.Background(), "bot-1", from, to, "", 2, next1)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page 2 has %d points, want 2", len(page2))
	}
	if next2 == "" {
		t.Fatal("page 2: want a non-empty next_page_token (1 row remains)")
	}
	if !page2[0].Time.After(page1[len(page1)-1].Time) {
		t.Error("page 2's first point is not strictly after page 1's last point -- pagination overlapped or went backwards")
	}

	page3, next3, err := q.Query(context.Background(), "bot-1", from, to, "", 2, next2)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page3) != 1 {
		t.Fatalf("page 3 has %d points, want 1 (the last remaining row)", len(page3))
	}
	if next3 != "" {
		t.Errorf("page 3: next_page_token = %q, want empty -- no rows remain", next3)
	}
}

func TestQueryLimitClampedToMaxLimit(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)
	base := time.Now().UTC().Add(-time.Hour)
	insertRow(t, pool, "bot-1", base, boolPtr(true), "g", "h", `{}`)

	q := New(pool)
	// A limit far beyond MaxLimit must not error or panic -- it's silently
	// clamped (see Query's doc comment); the fixture only has one row, so
	// this mainly proves the clamp path doesn't misbehave.
	points, _, err := q.Query(context.Background(), "bot-1", base.Add(-time.Second), base.Add(time.Hour), "", MaxLimit*10, "")
	if err != nil {
		t.Fatalf("Query with an oversized limit: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}
}

func TestStatusCountsValidAndInvalidWithinWindow(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	now := time.Now().UTC()
	insertRow(t, pool, "bot-1", now.Add(-30*time.Minute), boolPtr(true), "g", "h", `{}`)
	insertRow(t, pool, "bot-1", now.Add(-20*time.Minute), boolPtr(true), "g", "h", `{}`)
	insertRow(t, pool, "bot-1", now.Add(-10*time.Minute), boolPtr(false), "g", "h", `{}`)
	insertRow(t, pool, "bot-1", now.Add(-5*time.Minute), nil, "", "", `{}`)           // no_contract -- counts toward neither
	insertRow(t, pool, "bot-1", now.Add(-2*time.Hour), boolPtr(true), "g", "h", `{}`) // outside the 1h window

	q := New(pool)
	lastSeen, validCount, invalidCount, err := q.Status(context.Background(), "bot-1", time.Hour)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if validCount != 2 {
		t.Errorf("validCount = %d, want 2", validCount)
	}
	if invalidCount != 1 {
		t.Errorf("invalidCount = %d, want 1", invalidCount)
	}
	if lastSeen.IsZero() {
		t.Fatal("lastSeen is zero, want the most recent row's time")
	}
	wantLastSeen := now.Add(-5 * time.Minute)
	if lastSeen.Sub(wantLastSeen).Abs() > time.Second {
		t.Errorf("lastSeen = %v, want ~%v", lastSeen, wantLastSeen)
	}
}

func TestStatusNoRowsReturnsZeroValues(t *testing.T) {
	pool := testPool(t)
	setupTelemetryTable(t, pool)

	q := New(pool)
	lastSeen, validCount, invalidCount, err := q.Status(context.Background(), "never-seen-bot", time.Hour)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !lastSeen.IsZero() {
		t.Errorf("lastSeen = %v, want zero", lastSeen)
	}
	if validCount != 0 || invalidCount != 0 {
		t.Errorf("counts = %d/%d, want 0/0", validCount, invalidCount)
	}
}
