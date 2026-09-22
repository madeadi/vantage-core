package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultRetention matches migrations/0001_init.sql's baked-in policy -- the
// "sane default, not a decision to agonise over" from
// specs/mqtt_telemetry.specs.md's Ops section.
const DefaultRetention = 90 * 24 * time.Hour

// ApplyRetentionPolicy (re-)declares the telemetry hypertable's retention
// policy for retention (falling back to DefaultRetention when <= 0),
// letting a deployment override the migration's baked-in 90 days via
// config rather than hand-editing SQL. Removing and re-adding is
// TimescaleDB's own documented way to change an existing policy's
// interval -- add_retention_policy's if_not_exists only guards against
// creating a duplicate, it does not update an existing policy's interval.
//
// Requires TimescaleDB, same as migrations/0001_init.sql; this sandbox has
// no TimescaleDB-enabled Postgres available (see that file's doc comment),
// so this function is exercised by reading, not by a live test here --
// verify against a real TimescaleDB server before relying on it.
func ApplyRetentionPolicy(ctx context.Context, pool *pgxpool.Pool, retention time.Duration) error {
	if retention <= 0 {
		retention = DefaultRetention
	}
	if _, err := pool.Exec(ctx, `SELECT remove_retention_policy('telemetry', if_exists => true)`); err != nil {
		return fmt.Errorf("store: remove existing retention policy: %w", err)
	}
	interval := fmt.Sprintf("%d seconds", int64(retention.Seconds()))
	if _, err := pool.Exec(ctx,
		`SELECT add_retention_policy('telemetry', drop_after => $1::interval, if_not_exists => true)`, interval,
	); err != nil {
		return fmt.Errorf("store: add retention policy: %w", err)
	}
	return nil
}
