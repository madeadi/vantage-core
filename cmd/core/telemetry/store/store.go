// Package store batches and writes telemetry rows to Postgres/TimescaleDB.
// See specs/mqtt_telemetry.specs.md Step 12.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Row is one telemetry message, resolved by cmd/core/telemetry/mapping and
// validated by cmd/core/telemetry/validate, ready to write.
type Row struct {
	Time       time.Time
	ReceivedAt time.Time
	AgentID    string
	GroupID    string // "" -> SQL NULL (no group -- see registry.Entry)
	SchemaHash string // "" -> SQL NULL
	// Valid is nil when there was no contract to validate against (the
	// no_contract case -- see specs/mqtt_telemetry.specs.md), true/false
	// otherwise. Stored as a nullable boolean, not defaulted to either.
	Valid   *bool
	Fields  json.RawMessage // mapped projection; nil -> SQL NULL
	Payload json.RawMessage // required -- the full raw payload
}

// Config configures a Store. Zero values fall back to the documented
// defaults from specs/mqtt_telemetry.specs.md.
type Config struct {
	// BatchSize flushes the current batch once it reaches this many rows.
	// Default 100.
	BatchSize int
	// FlushInterval flushes the current batch on this schedule regardless
	// of size, so a low-traffic period doesn't leave rows sitting
	// unwritten indefinitely. Default 1s.
	FlushInterval time.Duration
	// QueueSize bounds the enqueue channel; Enqueue drops (and counts)
	// rather than blocking once it's full, so a persistence stall never
	// backs up into -- and blocks -- the ingest pipeline that feeds it.
	// Default 10000 (at the 10-agent/1Hz scale target, ~1000s of headroom).
	QueueSize int
	// MaxRetries bounds how many times a failed batch write is retried
	// (with exponential backoff starting at RetryBackoff) before the batch
	// is dropped and counted as failed rather than retried forever --
	// see specs/mqtt_telemetry.specs.md Step 12's "Done when": killing
	// Postgres mid-run must drop rows with a count, not deadlock ingest.
	// Default 3.
	MaxRetries int
	// RetryBackoff is the delay before the first retry; it doubles after
	// each subsequent attempt. Default 200ms.
	RetryBackoff time.Duration
}

func (c Config) withDefaults() Config {
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = time.Second
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 10000
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = 200 * time.Millisecond
	}
	return c
}

// Stats is a snapshot of the store's counters.
type Stats struct {
	Written uint64 // rows successfully written
	Dropped uint64 // rows dropped because the enqueue queue was full
	Failed  uint64 // rows dropped after MaxRetries write failures
}

// Store batches Rows and writes them to the telemetry hypertable. Run must
// be started in its own goroutine for Enqueue'd rows to ever be written.
type Store struct {
	pool *pgxpool.Pool
	cfg  Config

	queue chan Row

	written atomic.Uint64
	dropped atomic.Uint64
	failed  atomic.Uint64
}

// New returns a Store writing through pool.
func New(pool *pgxpool.Pool, cfg Config) *Store {
	cfg = cfg.withDefaults()
	return &Store{
		pool:  pool,
		cfg:   cfg,
		queue: make(chan Row, cfg.QueueSize),
	}
}

// Enqueue queues row for writing. Non-blocking: if the queue is full (a
// sustained persistence stall), row is dropped and counted rather than
// blocking the caller -- which, wired into the ingest pipeline, is the
// single consumer goroutine draining it (see cmd/core/telemetry/ingest);
// blocking here would stall ingestion of every other agent's telemetry too.
func (s *Store) Enqueue(row Row) {
	select {
	case s.queue <- row:
	default:
		s.dropped.Add(1)
		slog.Warn("telemetry store: queue full, dropping row", "agent_id", row.AgentID)
	}
}

// Run drains the queue, flushing a batch when it reaches cfg.BatchSize or
// cfg.FlushInterval elapses, whichever comes first, until ctx is done (which
// also flushes whatever's left). Run blocks -- call it in its own goroutine.
func (s *Store) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]Row, 0, s.cfg.BatchSize)
	flush := func(fctx context.Context) {
		if len(batch) == 0 {
			return
		}
		s.writeBatch(fctx, batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// ctx is already canceled here, so writeBatch's own retry loop
			// (which waits on ctx.Done() between attempts, and passes ctx
			// straight into the INSERT) would fail and bail out on the very
			// first attempt -- silently dropping the final batch without
			// even counting it as Failed. Flush it on a fresh, short-lived
			// context instead, so shutdown still gets a real attempt (with
			// retries) to persist rows already pulled off the queue.
			fctx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
			flush(fctx)
			cancel()
			return
		case row := <-s.queue:
			batch = append(batch, row)
			if len(batch) >= s.cfg.BatchSize {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		}
	}
}

// shutdownFlushTimeout bounds the final flush issued when Run's context is
// canceled -- long enough for writeBatch's retries to get a real attempt in,
// short enough that shutdown doesn't hang indefinitely if Postgres is down.
const shutdownFlushTimeout = 5 * time.Second

// Stats returns a snapshot of the store's counters.
func (s *Store) Stats() Stats {
	return Stats{
		Written: s.written.Load(),
		Dropped: s.dropped.Load(),
		Failed:  s.failed.Load(),
	}
}

// writeBatch writes batch, retrying with exponential backoff up to
// cfg.MaxRetries times before giving up and counting the whole batch as
// failed -- a dead database must not block Run (and so the ingest pipeline
// feeding it) forever.
func (s *Store) writeBatch(ctx context.Context, batch []Row) {
	backoff := s.cfg.RetryBackoff
	var lastErr error

	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
		}

		if lastErr = s.insertBatch(ctx, batch); lastErr == nil {
			s.written.Add(uint64(len(batch)))
			return
		}
		slog.Warn("telemetry store: batch write failed, retrying",
			"attempt", attempt+1, "max_attempts", s.cfg.MaxRetries+1, "rows", len(batch), "error", lastErr)
	}

	s.failed.Add(uint64(len(batch)))
	slog.Error("telemetry store: batch write failed after all retries, dropping rows",
		"rows", len(batch), "error", lastErr)
}

const insertColumns = "time, received_at, agent_id, group_id, schema_hash, valid, fields, payload"
const numColumns = 8

// insertBatch writes batch as one multi-row INSERT -- one statement, one
// round trip, regardless of batch size (see specs/mqtt_telemetry.specs.md's
// scale-target reasoning for why this, and not COPY, is the right tool
// here).
func (s *Store) insertBatch(ctx context.Context, batch []Row) error {
	if len(batch) == 0 {
		return nil
	}

	var sql strings.Builder
	sql.WriteString("INSERT INTO telemetry (")
	sql.WriteString(insertColumns)
	sql.WriteString(") VALUES ")

	args := make([]any, 0, len(batch)*numColumns)
	for i, row := range batch {
		if i > 0 {
			sql.WriteByte(',')
		}
		base := i * numColumns
		fmt.Fprintf(&sql, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8)
		args = append(args,
			row.Time, row.ReceivedAt, row.AgentID,
			nullableString(row.GroupID), nullableString(row.SchemaHash), row.Valid,
			nullableJSON(row.Fields), row.Payload,
		)
	}

	_, err := s.pool.Exec(ctx, sql.String(), args...)
	return err
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
