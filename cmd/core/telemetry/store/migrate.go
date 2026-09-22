package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationsFS embeds the telemetry database's own SQL migrations --
// PocketBase migrations only manage its embedded SQLite (see
// cmd/core/migrations), not this Postgres/TimescaleDB database, so it needs
// its own small runner rather than reusing PocketBase's.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every migration in migrations/*.sql, in filename order,
// that hasn't been applied to this database yet -- tracked in a
// schema_migrations table this function creates if missing. Each
// migration runs in its own transaction; a migration is recorded as
// applied only if its transaction commits, so a failure partway through
// is safe to retry (already-applied migrations are skipped) once the
// underlying problem is fixed. Safe to call on every startup: applying an
// already-fully-migrated database is a no-op.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrate(ctx, pool, migrationsFS, "migrations")
}

// migrate is Migrate's actual implementation, taking fsys/dir as parameters
// so tests can exercise the runner's own logic (idempotency, per-migration
// transactions, ordering) against a fixture that doesn't require a real
// TimescaleDB-enabled Postgres server, which the real embedded migrations
// do -- see migrations/0001_init.sql's own doc comment.
func migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, dir string) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("store: read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename = $1)`, name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("store: check migration %q: %w", name, err)
		}
		if applied {
			continue
		}

		sqlBytes, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return fmt.Errorf("store: read migration %q: %w", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("store: begin migration %q: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("store: apply migration %q: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("store: record migration %q: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("store: commit migration %q: %w", name, err)
		}

		slog.Info("telemetry store: applied migration", "file", name)
	}

	return nil
}
