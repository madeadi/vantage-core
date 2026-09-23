// Package query answers Step 13's "query telemetry" and "am I sending
// correct data" requirements by reading the telemetry table Step 12 writes
// to. See specs/mqtt_telemetry.specs.md Step 13.
package query

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultLimit/MaxLimit bound Query: at this project's 1Hz scale target an
// hour is 3600 raw points (fine to send), a day is 86k (bucket it) -- see
// specs/mqtt_telemetry.specs.md Step 13. MaxLimit is a hard ceiling applied
// regardless of what the caller requests, so a wide unbucketed range can't
// turn into a browser-killing response.
const (
	DefaultLimit = 1000
	MaxLimit     = 5000
)

// Point is one returned telemetry row (raw or bucketed).
type Point struct {
	AgentID    string
	Time       time.Time
	ReceivedAt time.Time
	GroupID    string // "" if the agent had no group at ingest time
	SchemaHash string // "" if there was no contract to validate against
	Valid      *bool  // nil if there was no contract to validate against
	Fields     []byte // jsonb, nil if no mapping projection was configured
	Payload    []byte // jsonb, always present
}

// Querier reads Step 12's telemetry table. A nil *Querier is valid and
// treated as "persistence is disabled" by callers (see
// cmd/core/controller/telemetry_connect.go) -- Step 13's reads must degrade
// gracefully, not panic, when a deployment has validation on but
// persistence off.
type Querier struct {
	pool *pgxpool.Pool
}

// New returns a Querier reading through pool.
func New(pool *pgxpool.Pool) *Querier {
	return &Querier{pool: pool}
}

// Query returns points for agentID in (from, to), ordered by time ascending,
// capped at limit (clamped to [1, MaxLimit], defaulting to DefaultLimit when
// <= 0). pageToken, when non-empty, resumes after the last point a prior
// call returned (see decodeToken) -- combine with the returned
// nextPageToken to page through a wide range.
//
// bucket == "": raw points, one row per stored message.
//
// bucket != "" (e.g. "1m", "5m", "1h"): one row per time_bucket(bucket,
// time) -- the LAST point in each bucket (by time), not an average: fields
// and payload are arbitrary jsonb, there's no generic way to average them,
// so this downsamples by picking a representative sample per bucket rather
// than aggregating every column. Requires TimescaleDB's time_bucket
// function, same as migrations/0001_init.sql's hypertable statements --
// this package's own test only exercises the bucket == "" path against a
// plain-Postgres fixture; see that migration's doc comment for why.
func (q *Querier) Query(ctx context.Context, agentID string, from, to time.Time, bucket string, limit int, pageToken string) ([]Point, string, error) {
	if agentID == "" {
		return nil, "", errors.New("query: agent_id is required")
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	after, err := decodeToken(pageToken)
	if err != nil {
		return nil, "", fmt.Errorf("query: invalid page_token: %w", err)
	}
	if !after.IsZero() && after.After(from) {
		from = after
	}

	var (
		sql  string
		args []any
	)
	if bucket == "" {
		sql = `
			SELECT time, received_at, agent_id, group_id, schema_hash, valid, fields, payload
			FROM telemetry
			WHERE agent_id = $1 AND time > $2 AND time < $3
			ORDER BY time ASC
			LIMIT $4`
		args = []any{agentID, from, to, limit + 1}
	} else {
		sql = `
			SELECT DISTINCT ON (time_bucket($5::interval, time))
			       time, received_at, agent_id, group_id, schema_hash, valid, fields, payload
			FROM telemetry
			WHERE agent_id = $1 AND time > $2 AND time < $3
			ORDER BY time_bucket($5::interval, time) ASC, time DESC
			LIMIT $4`
		args = []any{agentID, from, to, limit + 1, bucket}
	}

	rows, err := q.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var points []Point
	for rows.Next() {
		var p Point
		var groupID, schemaHash *string
		if err := rows.Scan(&p.Time, &p.ReceivedAt, &p.AgentID, &groupID, &schemaHash, &p.Valid, &p.Fields, &p.Payload); err != nil {
			return nil, "", fmt.Errorf("query: scan: %w", err)
		}
		if groupID != nil {
			p.GroupID = *groupID
		}
		if schemaHash != nil {
			p.SchemaHash = *schemaHash
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("query: %w", err)
	}

	var nextToken string
	if len(points) > limit {
		points = points[:limit]
		nextToken = encodeToken(points[limit-1].Time)
	}
	return points, nextToken, nil
}

// Status returns agentID's last-seen time (zero if never seen) and
// valid/invalid row counts over the trailing window, from the telemetry
// table. A row with a nil Valid (no_contract) counts toward neither.
func (q *Querier) Status(ctx context.Context, agentID string, window time.Duration) (lastSeen time.Time, validCount, invalidCount int, err error) {
	since := time.Now().Add(-window)
	var last *time.Time
	err = q.pool.QueryRow(ctx, `
		SELECT
			max(time),
			count(*) FILTER (WHERE valid IS TRUE),
			count(*) FILTER (WHERE valid IS FALSE)
		FROM telemetry
		WHERE agent_id = $1 AND time > $2
	`, agentID, since).Scan(&last, &validCount, &invalidCount)
	if err != nil {
		return time.Time{}, 0, 0, fmt.Errorf("query: status: %w", err)
	}
	if last != nil {
		lastSeen = *last
	}
	return lastSeen, validCount, invalidCount, nil
}

// encodeToken/decodeToken keep the page_token opaque to callers (per the
// existing ListTasks pagination convention in proto/api/v1/task.proto) while
// just being a base64'd RFC3339Nano timestamp underneath.
func encodeToken(t time.Time) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano)))
}

func decodeToken(token string) (time.Time, error) {
	if token == "" {
		return time.Time{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, string(b))
}
