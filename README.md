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

# Run individual components (hot reload requires air)
make dev-core        # core server
make dev-smallbot    # reference agent
make dev-sps-mr      # SPS mobile robot agent
make dev-sps-mission # SPS food delivery mission runner

# Run the full SPS stack (core + sps-mr + sps-mission) in parallel
make dev-sps

# Or run directly without hot reload
go run ./cmd/core -config cmd/core/core.config.yaml
go run ./cmd/smallbot -config smallbot.config.yaml
```

The core server listens on `:8080` (HTTP/Connect) and `:9090` (gRPC).  
Swagger UI is available at `http://localhost:8080/swagger/`.

## API

The external REST API is built with [ConnectRPC](https://connectrpc.com). All endpoints accept `Content-Type: application/json` and use `POST`.

| Endpoint | Description |
|---|---|
| `POST /api.v1.TaskService/CreateTask` | Dispatch a task to an agent |
| `POST /api.v1.TaskService/FindTask` | Look up a task by ID |
| `POST /api.v1.TaskService/ListTasks` | List tasks, optionally filter by `agentId` |
| `POST /api.v1.AgentService/ListAgents` | List all agents with online status and skills |
| `POST /api.v1.MissionService/ListMissions` | List all missions with online status |

Example:

```bash
curl -X POST http://localhost:8080/api.v1.TaskService/CreateTask \
  -H "Content-Type: application/json" \
  -d '{"agentId":"smallbot","type":"GO_HOME"}'

curl -X POST http://localhost:8080/api.v1.TaskService/ListTasks \
  -H "Content-Type: application/json" \
  -d '{}'
```

Proto definitions are in `proto/api/v1/`.

## Configuration

`core.config.yaml` controls:

- **`agents`** — pre-provisioned agents; each entry has an `id`, `name`, and `key` (device key used to register)
- **`grpc_listen_addr`** / **`grpc_advertise_addr`** — gRPC server binding and the address advertised to agents
- **`layouts`** — named spatial layouts with coordinate systems (`pixel` or `latlon`)
- **`agent_layouts`** — maps an agent to a layout with an affine transformation matrix and north offset
- **`missions`** — pre-provisioned missions; each has an `id`, `name`, and `key`

```yaml
agents:
  - id: smallbot
    key: dev-key-abc123
    name: Small Bot

missions:
  - id: from_kitchen
    name: SPS From Kitchen
    key: key_from_kitchen

grpc_listen_addr: ":9090"
grpc_advertise_addr: "localhost:9090"
```

## Agent Protocol

```
1. POST /agents/register
   Authorization: Bearer <device-key>
   → { token, grpc_addr }

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

## Mission Protocol

```
1. POST /missions/register
   Authorization: Bearer <mission-key>
   → { token, grpc_addr }

2. gRPC stream: MissionService.StreamMission
   Metadata: authorization: Bearer <token>, mission_id: <id>
   ← MissionServerMessage (task status updates)
   → CreateTask / CreateTaskResponse
```

## Architecture

```
cmd/core/
  main.go                    — server wiring (HTTP mux + gRPC server)
  ui.go                      — SSE-driven dashboard at /

internal/core/
  controller/
    agent_registry_http.go   — POST /agents/register
    agent_connect.go         — AgentService Connect handler (ListAgents)
    mission_controller.go    — POST /missions/register
    mission_connect.go       — MissionService Connect handler (ListMissions)
    task_connect.go          — TaskService Connect handler (Create/Find/List)
  service/
    agent_registry.go        — agent auth, online state, stream management
    task_dispatcher.go       — task persistence + gRPC dispatch
    mission_registry.go      — mission auth, online state, stream management
    mission_task_manager.go  — bridges mission CreateTask ↔ agent dispatch
    pose_listener.go         — pose telemetry aggregation
  repository/
    task_repo_memory.go      — in-memory TaskRepo implementation
  model/                     — Agent, Task, Mission domain types

pkg/
  agentsdk/                  — agent-side SDK (skill runner, service manager)
  util/                      — shared utilities

proto/
  agent/v1/                  — internal AgentService proto (StreamTasks, telemetry, pose)
  mission/v1/                — internal MissionService proto (StreamMission, CreateTask)
  api/v1/                    — external Connect API (task, agent, mission)
```

## Development

```bash
# Regenerate protobuf (agent/mission internal + api/v1 Connect)
make proto

# Regenerate Swagger docs (after any handler change)
swag init -g cmd/core/main.go -o docs

# Run with race detector
go run -race ./cmd/core -config cmd/core/core.config.yaml
```

## Adding an Agent

1. Add an entry to `core.config.yaml` under `agents` with a unique `id`, `name`, and `key`.
2. In the agent process, `POST /agents/register` with `Authorization: Bearer <key>` to obtain a token.
3. Open the gRPC streams (`StreamTasks`, `ReportTelemetry`) using the token.