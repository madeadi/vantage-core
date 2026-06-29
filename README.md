# Vantage OS — Core Backend

Central server and agent SDK for Vantage OS. Manages robot agent registration, task dispatch, telemetry, and pose tracking.

## Binaries

| Binary | Command | Purpose |
|--------|---------|---------|
| `cmd/core` | `go run ./cmd/core` | Central server — HTTP + gRPC |
| `cmd/smallbot` | `go run ./cmd/smallbot` | Reference agent implementation |
| `cmd/sps-mr` | `go run ./cmd/sps-mr` | SPS mobile robot agent |
| `cmd/mission-sps` | `go run ./cmd/mission-sps` | SPS food delivery mission runner |

## Quick Start

```bash
# Build everything
go build ./...

# Run core server (hot reload requires air)
make dev-core

# Run reference agent
make dev-smallbot

# Or run directly
go run ./cmd/core -config core.config.yaml
go run ./cmd/smallbot -config smallbot.config.yaml
```

The core server listens on `:8080` (HTTP) and `:9090` (gRPC).  
Swagger UI is available at `http://localhost:8080/swagger/`.

## Configuration

`core.config.yaml` controls:

- **`agents`** — pre-provisioned agents; each entry has an `id`, `name`, and `key` (device key used to register)
- **`grpc_listen_addr`** / **`grpc_advertise_addr`** — gRPC server binding and the address advertised to agents
- **`layouts`** — named spatial layouts with coordinate systems (`pixel` or `latlon`)
- **`agent_layouts`** — maps an agent to a layout with an affine transformation matrix and north offset

```yaml
agents:
  - id: smallbot
    key: dev-key-abc123
    name: Small Bot

grpc_listen_addr: ":9090"
grpc_advertise_addr: "localhost:9090"
```

## Agent Protocol

```
1. POST /agents/register
   Authorization: Bearer <device-key>
   → { agent_id, token, grpc_url }

2. gRPC stream: AgentService.StreamTasks
   Metadata: authorization: Bearer <token>, agent_id: <id>
   ← ServerMessage (task payload)
   → TaskAck (status updates)

3. gRPC stream: AgentService.ReportTelemetry
   → TelemetryEvent (sensors, battery, etc.)

4. gRPC stream: AgentService.ReportPoseTelemetry
   → PoseTelemetryEvent (position + heading)
```

On reconnect, core re-dispatches any active tasks to the agent automatically.

## Architecture

```
cmd/core/
  agent_registry.go      — agent auth, online state, task dispatch
  agent_grpc.go          — gRPC server (StreamTasks, telemetry, pose)
  agent_registry_http.go — HTTP handler for /agents/register
  task.go                — Task type and status machine
  task_repo_memory.go    — in-memory TaskRepo implementation
  task_updated_handler.go — updates task status from agent ACKs
  task_http.go           — POST /api/v1/tasks, GET /api/v1/tasks
  ui.go                  — SSE-driven dashboard at /
  main.go                — wiring

pkg/
  agentsdk/              — agent-side SDK (skill runner, service manager)
  pubsub/                — WebSocket pub/sub hub (legacy MQTT client unused)
  util/                  — shared utilities

proto/
  agent/v1/              — AgentService proto (tasks, telemetry, pose)
  mission/v1/            — MissionService proto
```

## Development

```bash
# Regenerate protobuf
make proto

# Regenerate Swagger docs (after any handler change)
swag init -g cmd/core/main.go -o docs

# Run with race detector
go run -race ./cmd/core -config core.config.yaml
```

## Adding an Agent

1. Add an entry to `core.config.yaml` under `agents` with a unique `id`, `name`, and `key`.
2. In the agent process, `POST /agents/register` with `Authorization: Bearer <key>` to obtain a token.
3. Open the gRPC streams (`StreamTasks`, `ReportTelemetry`) using the token.