# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./...

# Run with hot reload (requires air)
make dev-core        # runs cmd/core with core.config.yaml
make dev-mqtt-agent  # runs cmd/mqtt-agent-example, a minimal reference MQTT agent

# Run directly
go run ./cmd/core -config core.config.yaml
go run ./cmd/mqtt-agent-example -core-url http://127.0.0.1:8080 -key <device-key>

# Regenerate protobuf
make proto

# Regenerate Swagger docs (run after any handler change)
swag init -g cmd/core/main.go -o docs

# PocketBase CLI — forwarded to pbApp.RootCmd (run from the repo root)
go run ./cmd/core -config cmd/core/core.config.yaml migrate create "add_x"      # scaffold migration
go run ./cmd/core -config cmd/core/core.config.yaml migrate down 1              # revert last migration
go run ./cmd/core -config cmd/core/core.config.yaml superuser create <email> <pass>  # admin UI login
```

Nuke + rebuild the DB: stop the server, `rm -rf pb_data`, restart. Migrations
recreate every collection and re-seed dev fixtures + the `admin`/`changeme123`
app user; recreate the superuser with the command above (or via the `/_/` first-run screen).

## Architecture

Multiple binaries share a single module (`vantageos-core`):

- **`cmd/core`** — the central server. Runs HTTP on `:8080` and gRPC on `:9090`.
- **`cmd/mqtt-agent-example`** — minimal runnable reference agent proving the MQTT SDK (`pkg/agentsdk`) end-to-end; copy this as a starting point for a real agent.
- **`cmd/telemetry-schema`** — CLI for deriving/inspecting a telemetry JSON Schema from a Go struct.

Task dispatch and telemetry ingest are MQTT-based (see
`specs/mqtt_telemetry.specs.md`); pose streaming, the affine transform
lookup, and skill registration still use gRPC (`AgentService` on `:9090`).
An agent needs both transports.

### Communication flow

1. Agent POSTs `Bearer <device-key>` to `POST /agents/register` → receives `{ agent_id, token, grpc_url, broker_url, username, password, topic_prefix }` (`pkg/agentsdk.Register`)
2. Agent connects to the MQTT broker with `CleanSession=false`, QoS 1, and an LWT on `<prefix>/agent/<id>/event` (`pkg/agentsdk.NewAgent`)
3. Core publishes new tasks to `<prefix>/agent/<id>/task/new`; the agent's `TaskManager` runs the registered handler and publishes status to `<prefix>/agent/<id>/task/<task_id>/status`, deduping QoS-1 redeliveries against a bounded LRU of recently-seen task IDs
4. Agent publishes telemetry to `<prefix>/agent/<id>/telemetry`; core validates it against the agent's `agent_groups.telemetry_schema` and optionally persists it (Postgres/TimescaleDB)
5. Agent still opens the gRPC `AgentService.ReportPoseTelemetry` stream for pose, calls `GetTransformationMatrices` for the agent↔layout affine transform, and calls `ReportSkills` to register its skill inventory
6. A persistent MQTT session (`CleanSession=false`) means the broker itself queues an offline agent's tasks and redelivers them on reconnect — no gRPC-style reconnect-triggered re-dispatch is needed

### Configuration

`core.config.yaml` holds only bootstrap settings that must be known before the
embedded PocketBase starts: `grpc_listen_addr`, `grpc_advertise_addr`, and the
`pocketbase` block (PocketBase is now required — core exits if it's disabled).

Allowed **agents**, **agent groups**, **missions**, **layouts**, and **agent↔layout
transforms** live in PocketBase collections (`agents`, `agent_groups`, `missions`,
`layouts`, `agent_layouts`), edited via the admin UI at `http://127.0.0.1:8090/_/`
or the REST API by an `admin`-role user.

**Schema is managed by PocketBase Go migrations in `cmd/core/migrations/`** — one
`init()` per file calling `m.Register(up, down)`, applied in filename order and
recorded in `_migrations` so each runs once per DB.

- `1725840000_init_schema.go` — the `users` `username` + `role` (`admin`/`operator`/`viewer`) fields, the five config collections (all CRUD rules `@request.auth.role = "admin"`; superusers bypass), and the `agents.agent_group` → `agent_groups` relation (single, no cascade delete; expand with `?expand=agent_group`).
- `1725840001_seed_dev_data.go` — dev fixtures into any still-empty config collection + an `admin` / `changeme123` user when `users` is empty (change the password after first login).
- **Adding a column: write a new migration file** (`migrate create`, or edit schema in the admin UI in dev and Automigrate writes the file). Do not edit an applied migration.

`cmd/core/pocketbase.go`:
- `setupPocketBase` registers `migratecmd` (Automigrate on), bootstraps the app, then calls `app.RunAppMigrations()` before any collection is read (`apis.Serve` re-runs them, a no-op once applied).
- `bindUserRoleDefault` — runtime `OnRecordCreate` hook: first user with no role → `admin`, rest → `viewer`.
- `main.go` forwards extra args to `pbApp.RootCmd` (`migrate …`, `superuser …`) then exits.

`cmd/core/pbconfig.go` keeps only the runtime pieces:

- `loadAllowedAgents` / `loadMissions` / `loadAgentLayouts` populate the registries at startup.
- `watchConfig` binds `OnRecordAfter{Create,Update,Delete}Success` hooks so edits live-reload into the registries with no restart. A mission with a live gRPC stream keeps it until it reconnects; an agent's MQTT session is independent of this config reload — token removal only blocks new registrations.

### Key packages

- **`pkg/agentsdk`** — the MQTT agent SDK: `Agent`/`NewAgent` (connection lifecycle, LWT, lifecycle events), `TaskManager` (task dispatch, idempotent redelivery handling, manual acking), `Telemetry[T]` (schema derivation + publish loop), `Register` (device-key bootstrap). `pkg/agentsdk/agent_skill`, `task_handler`, `dummy_bot`, `slamtec`, `server`, and `service` are older gRPC-era robot-skill/task-handling building blocks kept for reference; no binary in this repo currently uses them.
- **`pkg/pubsub`** — legacy WebSocket pub/sub hub (unused by core, kept for reference).
- **`pkg/util`** — shared utilities.

### Inside `cmd/core`

- `AgentRegistry` — tracks allowed agents (pre-shared keys), skills, cameras, and MQTT presence (`MarkMQTTOnline`/`MarkMQTTOffline`, with a staleness timeout as a last-resort safety net — see `mqttPresenceStaleAfter`). `SetAllowedAgents` atomically swaps the allowed set (used by `watchConfig`); `MissionRegistry.SetAllowed` and `agentGRPCServer.SetLayouts` do the same for their config.
- `TaskRepo` / `TaskRepoMemory` — stores tasks; read methods return copies to avoid data races.
- `TaskDispatcher` — persists a task then publishes it to the agent's MQTT `task/new` topic; `MissionTaskManager.OnTaskUpdated` applies status updates decoded off the MQTT `task/status` topic (`cmd/core/task_mqtt.go`). gRPC task dispatch (`StreamTasks`) was removed once the MQTT path shipped (`specs/mqtt_telemetry.specs.md` Step 17).
- `agentGRPCServer` — gRPC server implementing `ReportPoseTelemetry`, `GetTransformationMatrices`, and `ReportSkills`. `StreamTasks`/`ReportTelemetry` were removed once MQTT task dispatch/telemetry ingest replaced them.

### Task dispatch invariants

- Tasks are saved to the repo **before** being published to MQTT (prevents dropped status updates on fast agents).
- Only one active task per agent is allowed; `SendTask` rejects with "agent is busy" if `GetActiveTasksByAgent` returns results.
- Both checks happen inside `TaskDispatcher.mu.Lock()` so they are atomic with respect to concurrent dispatch attempts.
- Task status can be redelivered at least once (MQTT QoS 1); `pkg/agentsdk.TaskManager` dedups on the agent side, and `MissionTaskManager.OnTaskUpdated` applying the same ack twice is itself a no-op.

### Swagger

Handlers use `swaggo/swag` annotations. After changing any handler, run `swag init` (see command above) to regenerate `docs/`. The spec is served at `/swagger/`.