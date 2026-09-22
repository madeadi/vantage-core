# MQTT Telemetry Pipeline

Expand `pkg/agentsdk` so an agent developer declares a telemetry shape once, has
it continuously validated by Vantage against a fleet-level contract, persisted
for replay, and inspected in the UI — then move agent telemetry and task
dispatch off gRPC onto MQTT.

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
- **Broker is Mosquitto.** The `%u` pattern ACLs below are Mosquitto syntax and
  can be taken literally.
- **Pose is out of scope.** It gets its own topic and its own validation model in
  a later spec. `ReportPoseTelemetry` stays on gRPC for now.
- **`MissionService` is out of scope.** It keeps its gRPC transport, which means
  `:9090` survives this work — see Phase B.

## Scale target

**10 agents at 1 Hz** — 10 msg/s, ~864k rows/day, ~315M/year if nothing is ever
pruned. This is small, and the spec should be honest about that rather than
build for a fleet that does not exist yet. It justifies *not* building:

- No `COPY`-based bulk insert — a batched multi-row `INSERT` is ample at 10/s.
- No worker pool — one consumer goroutine drains the ingest queue.
- No continuous aggregates — `time_bucket` at query time covers replay
  downsampling until rates are an order of magnitude higher.
- No space partitioning, no separate high-rate lanes.

TimescaleDB still earns its place for the hypertable, native compression and a
declarative retention policy, all of which are near-free to adopt now and
awkward to retrofit. Everything above is a one-line change if the fleet grows;
none of it is worth carrying today.

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

| Topic | Publisher | Retain | QoS | Scope |
|---|---|---|---|---|
| `<prefix>/agent/<id>/telemetry` | agent | no | 0 | Phase A |
| `<prefix>/agent/<id>/telemetry-schema` | agent | **yes** | 1 | Phase A |
| `<prefix>/agent/<id>/event` | agent (+ broker LWT) | **yes** | 1 | Phase A |
| `<prefix>/agent/<id>/task/new` | **core** | no | 1 | Phase B |
| `<prefix>/agent/<id>/task/<taskID>/status` | agent | no | 1 | Phase B |
| `<prefix>/agent/<id>/pose` | agent | no | 0 | **reserved** |

`pose` is reserved, not built — pose keeps its gRPC transport and gets its own
validation model in a later spec. The name is claimed here so that spec inherits
this layout instead of reopening it.

Core subscribes `<prefix>/agent/+/telemetry`, `+/telemetry-schema`, `+/event`,
and (Phase B) `<prefix>/agent/+/task/+/status`.

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
and `+/telemetry-schema`, extracting `agent_id` from the topic.

Critical, and independent of rate: **paho dispatches handlers on a single
goroutine by default.** Work done in the callback stalls every other message —
including the `event` topic that presence depends on. The callback must do
nothing but non-blocking enqueue onto a buffered channel. On full buffer, drop
and count (raising `telemetry_dropped` via Step 9's throttle) — never block the
broker connection.

One consumer goroutine drains the queue; at 10 msg/s a pool buys nothing. Size
the buffer for ~100s of headroom (1024 entries) so a brief DB stall is absorbed
rather than dropped.

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

Batch writer: multi-row `INSERT`, flushed on `batch_size` (100) or
`flush_interval` (1s), whichever comes first; retry with backoff; bounded queue
that drops (counted) rather than growing. Add Timescale's native compression and
a declarative retention policy — both cheap now, awkward to retrofit.

Per the scale target: no `COPY`, no continuous aggregates, no space
partitioning. At 10 msg/s a batched `INSERT` every second is one statement
carrying ten rows.

New dependency: `github.com/jackc/pgx/v5`. These migrations are plain SQL with a
small runner — PocketBase migrations only manage its SQLite.

*Files:* `internal/core/telemetry/store/*.go`, `.../store/migrations/*.sql`
*Done when:* toggling persistence off stops writes without disabling validation,
and killing Postgres mid-run drops rows with a count rather than deadlocking
ingest.

### 13. Core API: query, status, live

Following the existing ConnectRPC style (`proto/api/v1/`, `POST /api.v1.*`):

- `TelemetryService/QueryTelemetry` — `agent_id`, `from`, `to`, `limit`, optional
  `bucket` for downsampling via `time_bucket` at query time. Cursor-paginated,
  hard row ceiling. At 1 Hz an hour is 3600 points (send raw) and a day is 86k
  (bucket it), so the ceiling is what keeps a wide range from becoming a
  browser-killing response.
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
  as live so the two cannot drift. Ordering is by `received_at`, displayed time
  is the mapped `time` — a skewed agent clock must not make the scrub bar jump
  backwards.

  Replay renders **telemetry only**. An earlier draft also plotted a map track,
  which quietly assumed the pose stream; with pose out of scope, the map is
  drawn only when the group's mapping projects `x`/`y` into `fields`, using the
  existing `PixelMap` / `LayoutMap` components. If it does not, replay is table
  and chart. Nothing here reads the pose topic.

Keep `app-sidebar.tsx` and `App.tsx` routes in sync (`ui/CLAUDE.md` calls this out).

*Files:* `ui/src/pages/telemetry/*.tsx`, `ui/src/lib/telemetry.ts`,
`ui/src/hooks/use-telemetry.ts`, `ui/src/components/app-sidebar.tsx`, `ui/src/App.tsx`
*Done when:* a developer starts the example agent, watches live rows land, breaks
the payload, sees a violation with the failing path, then replays the window.

---

# Phase B — Move agent telemetry and tasks off gRPC

**`:9090` survives this work.** An earlier draft titled this phase "decommission
gRPC" and ended by deleting the listener. That is not achievable under the
agreed scope, for two independent reasons:

1. `proto/mission/v1` `MissionService.StreamMission` is a **second** gRPC service
   on the same listener, consumed by `cmd/sps_mission`. Out of scope.
2. `ReportPoseTelemetry` is an `AgentService` RPC, and pose is out of scope — so
   even `AgentService` itself cannot be deleted.

So Phase B moves the two surfaces this spec actually covers, and leaves the rest
of the gRPC server standing. `AgentService` ends up partially migrated, which is
worth stating plainly rather than discovering later.

| RPC | Disposition |
|---|---|
| `ReportTelemetry` | → MQTT `<id>/telemetry` (Phase A), then removed |
| `StreamTasks` | → MQTT `<id>/task/new` + `<id>/task/<taskID>/status` (Step 16) |
| `ReportPoseTelemetry` | **stays on gRPC** — out of scope |
| `GetTransformationMatrices` | **stays on gRPC** — fetched by the pose pipeline |
| `ReportSkills` | **stays on gRPC** — no reason to move it alone |

**Risk:** `cmd/sps-mr` is a working agent driving physical robots, and
`StreamTasks` is the control path — a dropped task means a robot does not move.
Recommend a short dual-run for the task path only (Step 16), deleted in Step 17.
Telemetry is read-only and can cut over directly.

### 15. Registration → broker credentials

`POST /agents/register` currently returns `{token, grpc_url}`. **Extend** rather
than replace: add `{broker_url, username, password, topic_prefix}` while keeping
`token` and `grpc_url`, since pose and missions still need the gRPC transport
(Step 17). The existing `agents.key` device key stays the bootstrap secret.

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

### 17. Remove the migrated RPCs

Delete only what has a working MQTT replacement: the `ReportTelemetry` and
`StreamTasks` RPCs from `proto/agent/v1`, their handlers in `cmd/core/grpc/`,
`pkg/agentsdk/service/stream_task.go` and `telemetry_service.go`, and the
Step 16 dual-run path.

Keep: the `:9090` listener, `grpc_listen_addr` / `grpc_advertise_addr`,
`MissionService` entirely, and the three `AgentService` RPCs above. Registration
returns both broker credentials **and** `grpc_url` — agents need both transports
until pose and missions move.

Update root `CLAUDE.md` (which currently describes gRPC as the only agent
transport) and `README.md` (already stale — it documents `cmd/smallbot` and
`cmd/mission-sps`, neither of which exists).

*Done when:* `cmd/sps-mr` reports telemetry and receives tasks over MQTT while
still streaming pose over gRPC, and no code path references the deleted RPCs.

---

## Ops (rides along with both phases)

- `core.config.yaml`: `mqtt:` block (broker, credentials, topic prefix, client
  id, QoS) and `telemetry:` block (persistence enabled, DSN, batch size, flush
  interval, retention, throttle window). Bootstrap-only, per `CLAUDE.md`.
- `docker-compose.yml` for local dev: Mosquitto + TimescaleDB, shipping the
  six-pattern ACL file above so the read/write split is exercised in dev rather
  than discovered in production. Include a test asserting an agent *cannot*
  publish to its own `task/new`.
- Retention default: **90 days** raw (~78M rows at the scale target, trivial for
  Timescale). A config knob, not a decision to agonise over — it is one policy
  statement to change, and compression makes the older end cheap.
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

Phase B   15 → 16 → 17             after Phase A proves MQTT in practice
```

Steps 10 and 11 are independent of the SDK track and can start immediately.
Steps 6–9 and 11–12 parallelise once 1–2 fix the topic and schema contracts.
Phase B should not start until Phase A has run telemetry over MQTT against real
agents — that is the evidence that moving the control path is safe.

## Open questions

None blocking. Two deferred, both safe to answer later:

1. Multi-tenant: is `topic_prefix` per-deployment or per-customer? Determines
   whether `agent_id` must be globally unique — and, now that the agent ID is
   the broker username, whether credentials must be unique across tenants too.
   Answerable before Step 15 without disturbing anything earlier.
2. Does the UI need to *author* group schemas beyond a JSON textarea? Step 2's
   CLI covers the derive-from-Go path; a hand-written contract is a Step 14
   nicety. Agent developers hold `admin`, so permissions are not the obstacle.

Retention is settled at a 90-day default (see Ops) rather than left open —
it is a config knob, and treating it as a blocking decision was overthinking.
