-- 0001_init creates the telemetry hypertable. Applied by the small runner in
-- cmd/core/telemetry/store/migrate.go -- PocketBase migrations only manage
-- its own embedded SQLite (see cmd/core/migrations), not this database.
--
-- Requires TimescaleDB installed on the target Postgres server: this is not
-- a plain-Postgres fallback path. The whole point of choosing Postgres +
-- TimescaleDB over the config collections' SQLite (see
-- specs/mqtt_telemetry.specs.md "Two datastores") is the hypertable,
-- native compression, and a declarative retention policy below -- a server
-- without the extension should fail this migration loudly, not silently
-- degrade to an un-chunked, uncompressed table.
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS telemetry (
  time        timestamptz NOT NULL,
  received_at timestamptz NOT NULL,
  agent_id    text        NOT NULL,
  group_id    text,
  schema_hash text,
  valid       boolean,        -- null = no contract to validate against
  fields      jsonb,          -- mapped projection (cmd/core/telemetry/mapping)
  payload     jsonb       NOT NULL
);

SELECT create_hypertable('telemetry', by_range('time'), if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS telemetry_agent_id_time_idx ON telemetry (agent_id, time DESC);

-- Compression and retention are cheap to declare now and awkward to retrofit
-- once a fleet has been running for a while -- see
-- specs/mqtt_telemetry.specs.md's scale-target reasoning. segmentby agent_id
-- keeps per-agent replay queries efficient after compression.
ALTER TABLE telemetry SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'agent_id',
  timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('telemetry', compress_after => INTERVAL '7 days', if_not_exists => TRUE);

-- 90-day default retention (see specs/mqtt_telemetry.specs.md Ops) -- a
-- config knob, not a decision to revisit per deployment; change the
-- interval below and re-run add_retention_policy if a deployment needs
-- something else.
SELECT add_retention_policy('telemetry', drop_after => INTERVAL '90 days', if_not_exists => TRUE);
