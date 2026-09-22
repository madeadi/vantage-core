# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./...

# Run with hot reload (requires air)
make dev-core       # runs cmd/core with core.config.yaml
make dev-smallbot   # runs cmd/smallbot with smallbot.config.yaml
make dev-sps-mr     # runs cmd/sps-mr with spsmr.config.yaml

# Run directly
go run ./cmd/core -config core.config.yaml
go run ./cmd/smallbot -config smallbot.config.yaml

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
- **`cmd/smallbot`** — reference agent implementation.
- **`cmd/sps-mr`** — SPS mobile robot agent.
- **`cmd/mission-sps`** — SPS food delivery mission runner.

### Communication flow

1. Agent POSTs `Bearer <device-key>` to `POST /agents/register` → receives `{ agent_id, token, grpc_url }`
2. Agent opens gRPC stream `AgentService.StreamTasks` with metadata `authorization: Bearer <token>` and `agent_id: <id>`
3. Core pushes tasks as `ServerMessage` over the stream; agent replies with `TaskAck` status updates
4. Agent opens `AgentService.ReportTelemetry` and `AgentService.ReportPoseTelemetry` streams for sensor data and pose
5. On reconnect, core re-dispatches any active tasks to the agent automatically

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
- `watchConfig` binds `OnRecordAfter{Create,Update,Delete}Success` hooks so edits live-reload into the registries with no restart. An agent/mission with a live gRPC stream keeps it until it reconnects — token removal only blocks new registrations.

### Key packages

- **`pkg/agentsdk`** — agent-side SDK: skill runner, service manager, task dispatcher used by agent binaries.
- **`pkg/pubsub`** — legacy WebSocket pub/sub hub (unused by core, kept for reference).
- **`pkg/util`** — shared utilities.

### Inside `cmd/core`

- `AgentRegistry` — tracks allowed agents (pre-shared keys), online agents, tokens, skills, and gRPC streams. Implements `Reconnector` to re-dispatch active tasks on reconnect. `SetAllowedAgents` atomically swaps the allowed set (used by `watchConfig`); `MissionRegistry.SetAllowed` and `agentGRPCServer.SetLayouts` do the same for their config.
- `TaskRepo` / `TaskRepoMemory` — stores tasks; read methods return copies to avoid data races.
- `TaskUpdatedHandlerMemory` — updates task status in the repo when a `TaskAck` arrives from an agent.
- `TelemetryListener` — handles inbound telemetry events from agents.
- `agentGRPCServer` — gRPC server implementing `StreamTasks`, `ReportTelemetry`, `ReportPoseTelemetry`, and `GetTransformationMatrices`.

### Task dispatch invariants

- Tasks are saved to the repo **before** being sent over the gRPC stream (prevents dropped ACKs on fast agents).
- Only one active task per agent is allowed; `SendTask` rejects with "agent is busy" if `GetActiveTasksByAgent` returns results.
- Both checks happen inside `as.mu.Lock()` so they are atomic with respect to concurrent dispatch attempts.

### Swagger

Handlers use `swaggo/swag` annotations. After changing any handler, run `swag init` (see command above) to regenerate `docs/`. The spec is served at `/swagger/`.