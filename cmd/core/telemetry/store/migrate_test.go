package store

import (
	"context"
	"os"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testDSN is the Postgres instance this package's tests run against. Set
// via TELEMETRY_TEST_DSN; tests skip (not fail) when it's unset, since a
// real Postgres/TimescaleDB server is an external dependency this package
// cannot assume every environment running `go test ./...` has available.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TELEMETRY_TEST_DSN")
	if dsn == "" {
		t.Skip("TELEMETRY_TEST_DSN not set -- skipping tests that need a real Postgres instance")
	}
	return dsn
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), testDSN(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("cannot reach test Postgres instance: %v", err)
	}
	return pool
}

// dropSchemaMigrations resets tracking state between tests sharing the same
// database, along with any test tables a fixture created.
func dropSchemaMigrations(t *testing.T, pool *pgxpool.Pool, extraTables ...string) {
	t.Helper()
	ctx := context.Background()
	for _, tbl := range append([]string{"schema_migrations"}, extraTables...) {
		if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS "+tbl); err != nil {
			t.Fatalf("drop %s: %v", tbl, err)
		}
	}
}

func TestMigrateAppliesInOrderAndIsIdempotent(t *testing.T) {
	pool := testPool(t)
	t.Cleanup(func() { dropSchemaMigrations(t, pool, "mig_test_a") })
	dropSchemaMigrations(t, pool, "mig_test_a")

	fsys := fstest.MapFS{
		"migrations/0001_create.sql": {Data: []byte(`CREATE TABLE mig_test_a (id int)`)},
		"migrations/0002_alter.sql":  {Data: []byte(`ALTER TABLE mig_test_a ADD COLUMN name text`)},
	}

	if err := migrate(context.Background(), pool, fsys, "migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Both migrations applied: the table exists with both columns.
	if _, err := pool.Exec(context.Background(), `INSERT INTO mig_test_a (id, name) VALUES (1, 'x')`); err != nil {
		t.Fatalf("insert into migrated table: %v", err)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != 2 {
		t.Errorf("schema_migrations has %d rows, want 2", count)
	}

	// Calling migrate again must be a no-op, not an error (e.g. from
	// re-running CREATE TABLE without IF NOT EXISTS).
	if err := migrate(context.Background(), pool, fsys, "migrations"); err != nil {
		t.Fatalf("second migrate call: %v", err)
	}
}

func TestMigrateOnlyAppliesNewMigrations(t *testing.T) {
	pool := testPool(t)
	t.Cleanup(func() { dropSchemaMigrations(t, pool, "mig_test_b") })
	dropSchemaMigrations(t, pool, "mig_test_b")

	first := fstest.MapFS{
		"migrations/0001_create.sql": {Data: []byte(`CREATE TABLE mig_test_b (id int)`)},
	}
	if err := migrate(context.Background(), pool, first, "migrations"); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	// A second call with an additional migration must apply only the new
	// one -- re-running 0001's CREATE TABLE (no IF NOT EXISTS here) would
	// fail if it were re-applied.
	second := fstest.MapFS{
		"migrations/0001_create.sql": {Data: []byte(`CREATE TABLE mig_test_b (id int)`)},
		"migrations/0002_alter.sql":  {Data: []byte(`ALTER TABLE mig_test_b ADD COLUMN extra int`)},
	}
	if err := migrate(context.Background(), pool, second, "migrations"); err != nil {
		t.Fatalf("second migrate (with a new file): %v", err)
	}

	if _, err := pool.Exec(context.Background(), `INSERT INTO mig_test_b (id, extra) VALUES (1, 2)`); err != nil {
		t.Fatalf("extra column was not added by the new migration: %v", err)
	}
}

func TestMigrateFailedMigrationIsNotRecordedAsApplied(t *testing.T) {
	pool := testPool(t)
	t.Cleanup(func() { dropSchemaMigrations(t, pool, "mig_test_c") })
	dropSchemaMigrations(t, pool, "mig_test_c")

	broken := fstest.MapFS{
		"migrations/0001_broken.sql": {Data: []byte(`THIS IS NOT VALID SQL AT ALL`)},
	}
	if err := migrate(context.Background(), pool, broken, "migrations"); err == nil {
		t.Fatal("migrate with invalid SQL: want an error, got nil")
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM schema_migrations WHERE filename = '0001_broken.sql'`).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 0 {
		t.Error("a failed migration was recorded as applied -- it must not be, or a later fix would never be able to re-run it")
	}
}
