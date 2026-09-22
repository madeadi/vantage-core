# MQTT Telemetry + gRPC Decommission

Expand `pkg/agentsdk` so an agent developer declares a telemetry shape once, has
it continuously validated by Vantage against a fleet-level contract, persisted
for replay, and inspected in the UI — then retire the gRPC agent transport in
favour of MQTT.

## Goal

An agent developer registers a telemetry contract on an agent group, writes an
agent against it, and gets a UI that tells them whether Vantage is receiving what
they think they are sending — live, and historically with replay.

---

## Decisions

**Resolved:**

- **Group schema is authoritative.** `agent_groups.telemetry_schema` is the
  single validation contract. An agent's own declared schema never overrides it.
- **MQTT replaces gRPC.** The `proto/agent/v1` `AgentService` and the `:9090`
  listener are removed once all five RPCs have MQTT/REST equivalents.
- **Agent-id-first topic layout.** The agent ID is one segment, high in the
  hierarchy, so broker ACL patterns can substitute it. See below.

**Consequences of "group schema wins" — these shape Phase A:**

1. Schema lives in PocketBase, not on a retained MQTT topic. The registry reads
   it via the existing `watchConfig` `OnRecordAfter*Success` live-reload pattern
   in `cmd/core/pbconfig.go` — a pattern already proven in this codebase, so the
   daemon no longer depends on retained-message durability to survive a restart.
2. Validation is resolved **per agent via its group**:
   `agent_id → agents.agent_group → agent_groups.telemetry_schema`. The relation
   already exists (single, no cascade, `?expand=agent_group`).
3. **New failure case: an agent with no group.** Nothing to validate against.
   Emit `no_contract` once per agent (throttled), pass the telemetry through to
   persistence marked `valid = null` rather than dropping it.
4. A schema edit in the UI invalidates the compiled schema for **every agent in
   that group at once**. The registry must key its compile cache by group +
   schema hash, not by agent.
5. `telemetry_mapping` is already per-group on the same record, so schema and
   mapping now share one authority model and one edit surface. Cleaner than
   splitting them.
6. The agent still *derives* a schema from its Go type, but only as an
   **advisory declaration**: core compares declared vs group contract and raises
   `schema_contract_mismatch`. This is the earliest possible warning — caught at
   connect time instead of after N bad messages — and it is the main thing that
   makes the developer-facing UI worth building.

## Topic layout (settled)

The old layout nested schema *under* telemetry
(`<prefix>/agent/telemetry/schema/<id>`), so `.../telemetry/#` mixed both
streams, an agent whose ID was literally `schema` collided, and the agent ID sat
at varying depths — which meant broker ACL patterns could not substitute it.
Replaced by:

| Topic | Publisher | Retain | QoS |
|---|---|---|---|
| `<prefix>/agent/<id>/telemetry` | agent | no | 0 |
| `<prefix>/agent/<id>/telemetry-schema` | agent | **yes** | 1 |
| `<prefix>/agent/<id>/pose` | agent | no | 0 |
| `<prefix>/agent/<id>/event` | agent (+ broker LWT) | **yes** | 1 |
| `<prefix>/agent/<id>/task/new` | **core** | no | 1 |
| `<prefix>/agent/<id>/task/<taskID>/status` | agent | no | 1 |

Core subscribes `<prefix>/agent/+/telemetry`, `+/telemetry-schema`, `+/pose`,
`+/event`, and `<prefix>/agent/+/task/+/status`.

This is a breaking change taken now, while there are zero deployed publishers.
No compatibility shim, no dual-topic period — Step 1 changes the builders and
that is the whole migration.

**ACL correction.** An earlier draft claimed "one rule per agent:
`<prefix>/agent/<id>/#`". That is wrong, and the reason is worth recording: a
single blanket rule grants the agent *write* on everything under its prefix,
including its own `task/new` — letting an agent dispatch tasks to itself.
Reads and writes must be split. With the broker username set to the agent ID
(Step 15), Mosquitto patterns substitute `%u`:

```
pattern write <prefix>/agent/%u/telemetry
pattern write <prefix>/agent/%u/telemetry-schema
pattern write <prefix>/agent/%u/pose
pattern write <prefix>/agent/%u/event
pattern write <prefix>/agent/%u/task/+/status
pattern read  <prefix>/agent/%u/task/new
```

Six patterns rather than one, but each is a single static rule covering every
agent — no per-agent ACL entries to generate or garbage-collect. That is the
actual win from putting the ID in a fixed position, and it is unavailable under
the old layout at any number of rules.

**Two datastores.** Events + config stay in PocketBase (low volume, gives
the UI realtime for free). Telemetry rows go to Postgres/TimescaleDB. Real
operational cost — two DBs to run and back up — but telemetry in SQLite will not
survive replay at any useful rate.

---

## Current state

Already in place:

- `agent.go` / `task_manager.go` / `topic.go` / `event.go` — MQTT agent with LWT,
  event publishing, task-dispatch subscriber. Builds, but **nothing calls
  `agentsdk.NewAgent`** yet.
- `github.com/google/jsonschema-go v0.4.3` is a **direct dependency**, already
  used by `task_handler/*.go` via `jsonschema.For[T](nil)`. Same trick works for
  telemetry.
- `agent_groups.telemetry_schema` (JSON) — migration `1788826201_updated_agent_groups.go`.
- UI CRUD for agent groups; Leaflet `PixelMap` / `LayoutMap` that replay reuses.

Bugs found while surveying:

- **`agent_groups.telemetry_mapping` has no migration.** `ui/src/lib/agent-groups.ts`
  and `agent-group-sheet.tsx` read and write it, but no migration adds the field,
  so writes are silently dropped. Fixed in Step 10 — independent of everything
  else, fixable today.
- `TelemetrySchema` is a stub; `PublishTelemetrySchema()` logs "not implemented".
- `Topic.Telemetry()`, `TelemetrySchema()`, `TaskStatus()`, `NewTaskRequest()`
  are never called.
- `Agent.Stop()` returns early when `stopTelemetryChan == nil` — an agent that
  never started a telemetry loop **never publishes `offline` and never
  disconnects**.
- `Agent.Register()` writes `a.Client` and `a.TaskManager.mqtt` from a goroutine
  outside `a.mu`; `NewTaskManager` is constructed with a `nil` client.

---

# Phase A — Telemetry on MQTT

### 1. Topic layout + retained/QoS semantics

Rework `topic.go` to the settled layout above, including the Phase B topics so
it is done once. Validate `topic_prefix` (no leading/trailing slash, no
wildcards); reject agent IDs containing `+`, `#`, `/`.

Agent-ID validation is load-bearing now, not hygiene: the ID is a topic segment
*and* the ACL substitution key, so an ID containing `/` or a wildcard would let
one agent's credentials match another's topics. Validate on the **core** side at
registration too (Step 15) — an agent validating its own ID proves nothing.

Carry the retain/QoS column from the table into the builders' doc comments, and
add subscriber-side helpers for the wildcard patterns core needs so the two
sides cannot drift.

*Files:* `pkg/agentsdk/topic.go` + table-driven test
*Done when:* builders emit the new layout and reject malformed IDs.

### 2. Schema derivation + contract authoring workflow

The group schema is authoritative, but hand-writing JSON Schema into a textarea
is a bad developer experience. Close the loop: derive the schema from the Go
type, then give the developer a way to get it **into** the group record.

- `jsonschema.For[T](nil)` → canonicalised (sorted-key) JSON → sha256, first 16
  hex as `schema_hash`. Stable across marshal ordering.
- `cmd/telemetry-schema` — tiny CLI that prints the derived schema for a type so
  it can be pasted into the UI, with `--push` writing it straight to the group
  record via the PocketBase API.

This keeps "the Go struct is the contract" ergonomics while respecting that
PocketBase holds the authoritative copy.

*Files:* `pkg/agentsdk/telemetry_schema.go`, `cmd/telemetry-schema/main.go`
*Done when:* a struct round-trips to a stable schema + hash twice identically,
and `--push` updates a group record.

### 3. SDK: typed telemetry publisher

Replace the `TelemetryPublisher` interface — its current shape
(`PublishTelemetry() error`) forces every implementer to close over the `Agent`
to publish anything, and centralises nothing.

```go
type Telemetry[T any] struct { /* agent, schema, hash, seq */ }

func NewTelemetry[T any](a *Agent) (*Telemetry[T], error)
func (t *Telemetry[T]) SchemaJSON() []byte
func (t *Telemetry[T]) PublishSchema() error   // retained, advisory
func (t *Telemetry[T]) Publish(v T) error
func (t *Telemetry[T]) StartLoop(ctx context.Context, every time.Duration, src func(context.Context) (T, error))
```

`StartLoop` is context-cancellable and non-blocking (spawns and returns) — today
`StartTelemetryLoop` blocks its caller.

*Files:* `pkg/agentsdk/telemetry.go` (new), `agent.go`
*Done when:* an agent publishes its advisory schema on connect and telemetry on
a ticker.

### 4. SDK lifecycle fixes

Folded in here because Step 3 touches the same lifecycle:

- `Stop()` publishes `offline` and disconnects **unconditionally**.
- Guard `a.Client` / `a.TaskManager.mqtt` with `a.mu`; give `TaskManager` a
  `SetClient` instead of constructing with `nil` and patching the field.
- Re-publish the advisory schema in `OnConnect` so it survives broker restart.
- Publish `online` on connect, distinct from the one-shot `registered`.

*Files:* `pkg/agentsdk/agent.go`, `task_manager.go`
*Done when:* `go test -race` passes connect → publish → drop → reconnect against
an embedded broker.

### 5. Reference agent

A minimal runnable agent proving the loop end-to-end: define a telemetry struct,
register, publish schema, publish on a ticker, handle one task. Extend
`pkg/agentsdk/dummy_bot/` or add `cmd/mqtt-agent-example/`. This is what an agent
developer copies.

*Files:* `cmd/mqtt-agent-example/main.go`, `Makefile` target
*Done when:* `make dev-mqtt-agent` publishes schema + telemetry to a local broker.

### 6. Core: MQTT ingest daemon

New `internal/core/telemetry/ingest`. Subscribes `<prefix>/agent/+/telemetry`
(and `+/telemetry-schema`, `+/pose`), extracting `agent_id` from the topic.

Critical: **paho dispatches handlers on a single goroutine by default.** Work
done in the callback stalls every other message. The callback must do nothing but
non-blocking enqueue onto a buffered channel drained by a worker pool. On full
buffer, drop and count (raising `telemetry_dropped` via Step 9's throttle) —
never block the broker connection.

Includes a fan-out registry so Step 13's SSE endpoint can tap the live stream
without opening a second broker connection.

*Files:* `internal/core/telemetry/ingest/*.go`, wired in `cmd/core/main.go`
*Done when:* ingests at a configured rate with bounded memory, reporting queue
depth and drop count.

### 7. Core: group schema registry

Loads `telemetry_schema` from `agent_groups`, compiles once per
`(group_id, schema_hash)`, resolves `agent_id → group → compiled schema`. Live
reload via the `watchConfig` hooks in `cmd/core/pbconfig.go`.

Handles the cases from **Consequences** above: agent with no group
(`no_contract`), group with no schema set (same), schema edited mid-flight
(recompile, invalidate the whole group, emit `contract_changed`), and the
advisory declared-vs-contract comparison feeding Step 8.

*Files:* `internal/core/telemetry/registry/*.go`, `cmd/core/pbconfig.go`
*Done when:* editing a group schema in the UI changes validation behaviour with
no restart, and compiles happen once per distinct hash.

### 8. Core: validator

Validates each payload against the group's compiled schema. Produces a structured
result, not a bool — the UI must show the developer *which field* is wrong:

```go
type Violation struct {
    AgentID    string
    GroupID    string
    SchemaHash string
    Kind       string   // missing_required | type_mismatch | constraint |
                        // not_json | no_contract | schema_contract_mismatch | bad_timestamp
    Paths      []string // JSON Pointers into the payload
    Detail     string
    Sample     json.RawMessage // truncated offending payload
}
```

`Signature()` = stable hash of `(Kind, sorted Paths)`. This is Step 9's throttle
key, so a robot sending the same wrong field 100×/s yields one event, not 100.

*Files:* `internal/core/telemetry/validate/*.go`
*Done when:* unit tests cover each `Kind`, and signatures are stable across
repeated identical failures.

### 9. Core: event throttling + aggregation

Requirement (1)'s "throttle/sample so it does not flood the db".

Per `(agent_id, signature)`: emit the **first** occurrence immediately, suppress
for a window (default 60s) while counting, then emit one summary carrying
`count`, `first_seen`, `last_seen`. Bound memory with an LRU capped at N
signatures per agent (default 50) — an agent emitting unique violations must not
grow the map without limit. Count and report evictions.

Config: window, per-agent signature cap, global events/sec ceiling.

*Files:* `internal/core/telemetry/events/*.go`
*Done when:* a 10k-bad-message burst produces ≤2 rows with a correct count.

### 10. PocketBase migrations

Never edit an applied migration (root `CLAUDE.md`). Three new ones:

1. **`agent_groups.telemetry_mapping`** (JSON) — fixes the live bug where the UI
   writes a field that does not exist.
2. **`agent_events`** — `agent_id`, `type`, `severity`, `kind`, `signature`,
   `detail`, `sample` (JSON), `count`, `first_seen`, `last_seen`; indexed on
   `(agent_id, last_seen)`. Write rule `@request.auth.role = "admin"`,
   operator-readable.
3. **`telemetry_settings`** — per-group persistence toggle, so requirement (3)'s
   on/off is runtime-settable. The DSN stays in `core.config.yaml` (bootstrap-only).

Regenerate `ui/src/lib/pocketbase-types.ts` (pocketbase-typegen).

*Files:* `cmd/core/migrations/*.go`, `ui/src/lib/pocketbase-types.ts`
*Done when:* `rm -rf pb_data` + restart recreates everything and UI edits to
`telemetry_mapping` persist.

### 11. Mapping layer

Requirement (4). Per-group, resolved before persistence:

```json
{
  "agent_id": "$.robot.serial",
  "timestamp": "$.header.stamp",
  "timestamp_format": "rfc3339 | unix_ms | unix_s",
  "fields": { "x": "$.pose.position.x", "y": "$.pose.position.y" }
}
```

Defaults when unset (the common case): `agent_id` from the topic segment,
timestamp from broker receive time. `fields` is an optional projection promoting
hot values to real columns for cheap replay queries, full payload still in `jsonb`.

Store **both** mapped timestamp and `received_at` — agent clocks skew and replay
needs a fallback ordering. Reject mapped timestamps outside a sane window (±24h)
as `bad_timestamp` rather than writing garbage into a hypertable.

*Files:* `internal/core/telemetry/mapping/*.go`
*Done when:* dotted-path extraction and all three time formats are unit-tested,
including missing-path and wrong-type cases.

### 12. Persistence: Postgres + TimescaleDB

Requirement (3). Off by default; enabled per config + per-group toggle.

```sql
CREATE TABLE telemetry (
  time        timestamptz NOT NULL,
  received_at timestamptz NOT NULL,
  agent_id    text        NOT NULL,
  group_id    text,
  schema_hash text,
  valid       boolean,          -- null = no contract to validate against
  fields      jsonb,            -- mapped projection
  payload     jsonb   NOT NULL
);
SELECT create_hypertable('telemetry', 'time');
CREATE INDEX ON telemetry (agent_id, time DESC);
```

Batch writer: flush on `batch_size` or `flush_interval`, whichever first;
`COPY`-based insert; retry with backoff; bounded queue that drops (counted)
rather than growing. Compression + configurable retention policy; a continuous
aggregate for downsampled replay over long ranges.

New dependency: `github.com/jackc/pgx/v5`. These migrations are plain SQL with a
small runner — PocketBase migrations only manage its SQLite.

*Files:* `internal/core/telemetry/store/*.go`, `.../store/migrations/*.sql`
*Done when:* toggling persistence off stops writes without disabling validation;
a sustained-write benchmark shows stable memory.

### 13. Core API: query, status, live

Following the existing ConnectRPC style (`proto/api/v1/`, `POST /api.v1.*`):

- `TelemetryService/QueryTelemetry` — `agent_id`, `from`, `to`, `limit`, optional
  `bucket` for downsampling. Cursor-paginated, hard row ceiling.
- `TelemetryService/GetTelemetryStatus` — group contract, `schema_hash`,
  last-seen, valid/invalid counts over a window, recent violations. This is what
  answers "am I sending correct data".
- `GET /telemetry/live?agent_id=` — **SSE**, fed from Step 6's fan-out. SSE
  rather than PocketBase realtime because telemetry does not live in PocketBase;
  core already serves an SSE dashboard, so the pattern exists.

Auth via the PocketBase token; operator reads, admin changes settings. Run
`swag init` after handler changes.

*Files:* `proto/api/v1/telemetry.proto`, `internal/core/controller/telemetry_*.go`
*Done when:* all three are callable and `make proto` regenerates cleanly.

### 14. UI: contract health, live, replay

Requirement (2). New sidebar section — and while there, fix the two sidebar
entries (`/video-stream`, `/remote-control`) that have **no matching route** in
`App.tsx` and currently 404.

- **Contract & health** (`/telemetry/:agentId`) — the group contract
  pretty-printed, schema hash, last-seen, valid/invalid rate, and a violation
  list showing the failing JSON Pointer beside the offending sample payload. Call
  out `schema_contract_mismatch` prominently: the agent's declared shape
  disagreeing with its group's contract is the highest-value early warning.
- **Live** (`/telemetry/:agentId/live`) — SSE subscription, rolling capped buffer
  (~500 rows), each row marked valid/invalid, pause/resume.
- **Replay** (`/telemetry/:agentId/replay`) — agent picker + time range, then
  play / pause / scrub / speed (1×, 5×, 30×). Replay drives the *same* renderer
  as live so the two cannot drift. When the mapping projects `x`/`y`, plot the
  track on the existing `PixelMap` / `LayoutMap` components.

Keep `app-sidebar.tsx` and `App.tsx` routes in sync (`ui/CLAUDE.md` calls this out).

*Files:* `ui/src/pages/telemetry/*.tsx`, `ui/src/lib/telemetry.ts`,
`ui/src/hooks/use-telemetry.ts`, `ui/src/components/app-sidebar.tsx`, `ui/src/App.tsx`
*Done when:* a developer starts the example agent, watches live rows land, breaks
the payload, sees a violation with the failing path, then replays the window.

---

# Phase B — Decommission gRPC

`AgentService` has **five** RPCs, not just telemetry. Each needs a home before
`:9090` can go. Two are unary request/response, which MQTT models badly — those
belong on REST/Connect, not on a correlation-ID topic pair.

| RPC | Replacement |
|---|---|
| `ReportTelemetry` | MQTT `<id>/telemetry` — Phase A |
| `ReportPoseTelemetry` | MQTT `<id>/pose` — Step 17 |
| `StreamTasks` | MQTT `<id>/task/new` + `<id>/task/<taskID>/status` — Step 16 |
| `GetTransformationMatrices` | REST/Connect — Step 18 |
| `ReportSkills` | REST/Connect — Step 18 |

**Risk:** `cmd/sps-mr` and `cmd/sps_mission` are working agents driving physical
robots, and `StreamTasks` is the control path — a dropped task means a robot does
not move. Recommend a short dual-run for the task path only (Step 16), deleted in
Step 18. Telemetry and pose are read-only and can cut over directly.

### 15. Registration → broker credentials

`POST /agents/register` currently returns `{token, grpc_url}`. Replace with
`{broker_url, username, password, topic_prefix, agent_id}`, keeping the existing
`agents.key` device-key model as the bootstrap secret.

Issue **per-agent broker credentials with the username set to the agent ID** —
that is what makes the `%u` ACL patterns above resolve. A shared broker account
makes spoofing trivial, since the agent ID is otherwise just a topic segment any
publisher can type. Re-validate the agent ID here (Step 1) before it becomes a
credential.

*Files:* `internal/core/controller/agent_registry_http.go`, `pkg/agentsdk/registration.go`
*Done when:* an agent bootstraps from a device key to a working scoped broker
session.

### 16. Task dispatch over MQTT

Wire the unused `task/status` topic and preserve the invariants in root
`CLAUDE.md`: tasks saved to the repo **before** publish; one active task per
agent; both checks atomic under `as.mu`.

Two semantics genuinely change:

- **At-least-once.** QoS 1 can redeliver, so handlers must be idempotent, keyed
  on task ID. gRPC streams gave exactly-once-ish delivery for free; this does not.
- **Presence.** `AgentRegistry` currently infers "online" from an open stream.
  That signal disappears. Replace with LWT + birth message on `<id>/event`, plus
  a staleness timeout.

One thing gets *simpler*: `Reconnector`'s active-task re-dispatch can be replaced
by a persistent session (`CleanSession=false`, QoS 1) letting the broker queue
tasks for an offline agent. That deletes custom reconnect code rather than
porting it.

*Files:* `pkg/agentsdk/task_manager.go`, `cmd/core/service/agent_registry.go`,
`cmd/core/service/task_dispatcher.go`
*Done when:* a task survives an agent restart mid-dispatch, and duplicate
delivery executes once.

### 17. Pose telemetry over MQTT

Port `ReportPoseTelemetry` to `<prefix>/agent/<id>/pose`, feeding the existing
`PoseListener`. Higher rate than telemetry, so it gets its own topic and its own
ingest lane — pose volume must not evict telemetry from the shared queue. The
Monitoring UI's layout plotting must keep working throughout.

*Files:* `pkg/agentsdk/service/stream_pose.go` → `pkg/agentsdk/pose.go`,
`cmd/core/service/pose_listener.go`
*Done when:* poses render on `Monitoring` from the MQTT path with gRPC disabled.

### 18. Delete gRPC

Move the two unary RPCs to REST/Connect first: `GetTransformationMatrices` is a
startup config fetch (`stream_pose.go` calls it once before streaming), so it fits
the PocketBase REST API or a Connect endpoint naturally; `ReportSkills` becomes a
Connect call at registration.

Then delete: `proto/agent/v1`, `cmd/core/grpc/`, `pkg/agentsdk/server/`,
`pkg/agentsdk/service/stream_*.go` and `telemetry_service.go`, the `:9090`
listener, `grpc_listen_addr` / `grpc_advertise_addr` from config, the Step 16
dual-run path, and the now-unused gRPC deps from `go.mod`. Update root
`CLAUDE.md` and `README.md` (already stale — it documents `cmd/smallbot` and
`cmd/mission-sps`, neither of which exists).

*Done when:* `cmd/sps-mr` and `cmd/sps_mission` run with no gRPC, and
`grep -r grpc` is clean outside vendor.

---

## Ops (rides along with both phases)

- `core.config.yaml`: `mqtt:` block (broker, credentials, topic prefix, client
  id, QoS) and `telemetry:` block (persistence enabled, DSN, batch size, flush
  interval, retention, throttle window). Bootstrap-only, per `CLAUDE.md`.
- `docker-compose.yml` for local dev: Mosquitto + TimescaleDB, shipping the
  six-pattern ACL file above so the read/write split is exercised in dev rather
  than discovered in production. Include a test asserting an agent *cannot*
  publish to its own `task/new`.
- Integration test: embedded broker + ephemeral Timescale; agent publishes good
  then bad telemetry; assert throttling and row landing.

## Sequencing

```
Phase A   1 → 2 → 3 → 4 → 5        SDK track (blocks 6-9)
                  ↓
          6 → 7 → 8 → 9            validation
          10                       migrations   ← start now, fixes a live bug
          11 → 12                  persistence
                  ↓
          13 → 14                  API + UI

Phase B   15 → 16 → 17 → 18        after Phase A proves MQTT under load
```

Steps 10 and 11 are independent of the SDK track and can start immediately.
Steps 6–9 and 11–12 parallelise once 1–2 fix the topic and schema contracts.
Phase B should not start until Phase A has run telemetry over MQTT at production
rate — that is the evidence that moving the control path is safe.

## Open questions

1. Retention: how long does raw telemetry live before the continuous aggregate
   takes over? Drives disk sizing.
2. Multi-tenant: is `topic_prefix` per-deployment or per-customer? Determines
   whether `agent_id` must be globally unique — and, now that the agent ID is
   the broker username, whether credentials are unique across tenants too.
3. Does the UI need to *author* group schemas beyond a JSON textarea (Step 2's
   CLI covers the derive-from-Go path, but not a hand-written contract)?
