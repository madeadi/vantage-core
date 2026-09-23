# Vantage OS — Core Backend

Central server and agent SDK for Vantage OS. Manages robot agent registration,
task dispatch, telemetry (MQTT), and pose tracking (gRPC).

## Binaries

| Binary | Command | Purpose |
|--------|---------|---------|
| `cmd/core` | `go run ./cmd/core` | Central server — HTTP + gRPC + MQTT |
| `cmd/mqtt-agent-example` | `go run ./cmd/mqtt-agent-example` | Minimal reference agent proving the MQTT SDK end-to-end — copy this to build a real agent |
| `cmd/telemetry-schema` | `go run ./cmd/telemetry-schema` | CLI for deriving/inspecting a telemetry JSON Schema from a Go struct |

## Quick Start

```bash
# Build everything
go build ./...

# Run individual components (hot reload requires air)
make dev-core        # core server
make dev-mqtt-agent  # reference MQTT agent

# Or run directly without hot reload
go run ./cmd/core -config cmd/core/core.config.yaml
go run ./cmd/mqtt-agent-example -core-url http://127.0.0.1:8080 -key <device-key>
```

The core server listens on `:8080` (HTTP/Connect), `:9090` (gRPC), and
connects out to an MQTT broker when `mqtt.enabled: true` in its config.
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
  -d '{"agentId":"mqtt-agent-example","type":"PING"}'

curl -X POST http://localhost:8080/api.v1.TaskService/ListTasks \
  -H "Content-Type: application/json" \
  -d '{}'
```

Proto definitions are in `proto/api/v1/`.

## Configuration

`core.config.yaml` holds only bootstrap settings needed before the embedded
PocketBase starts: `grpc_listen_addr` / `grpc_advertise_addr`, the `mqtt`
block (broker URL, topic prefix — off by default), the `telemetry` block
(Postgres/TimescaleDB persistence — off by default), and the `pocketbase`
block. Allowed agents, agent groups, missions, layouts, and agent↔layout
transforms are config-collection records in PocketBase (`agents`,
`agent_groups`, `missions`, `layouts`, `agent_layouts`), edited via the admin
UI at `http://127.0.0.1:8090/_/` or the REST API by an `admin`-role user —
see `CLAUDE.md` for the migration files that define their schema.

```yaml
grpc_listen_addr: ":9090"
grpc_advertise_addr: "localhost:9090"

mqtt:
  enabled: true
  broker_url: "tcp://127.0.0.1:1883"
  topic_prefix: "vantageos"

pocketbase:
  enabled: true
```

## Agent Protocol

```
1. POST /agents/register
   Authorization: Bearer <device-key>
   → { agent_id, token, grpc_addr, broker_url, username, password, topic_prefix }

2. MQTT connect: CleanSession=false, QoS 1, LWT on <prefix>/agent/<id>/event
   → publishes retained "registered"/"online" events on connect

3. MQTT: core publishes tasks to <prefix>/agent/<id>/task/new
   ← agent's TaskManager runs the registered handler
   → agent publishes status to <prefix>/agent/<id>/task/<task_id>/status

4. MQTT: agent publishes telemetry to <prefix>/agent/<id>/telemetry
   → core validates against agent_groups.telemetry_schema, optionally persists it

5. gRPC stream: AgentService.ReportPoseTelemetry
   → PoseTelemetryEvent (position + heading)

6. gRPC unary: AgentService.GetTransformationMatrices, AgentService.ReportSkills
```

A persistent MQTT session (`CleanSession=false`) means the broker itself
queues an offline agent's tasks and redelivers them on reconnect — QoS 1
delivery is at-least-once, so `pkg/agentsdk.TaskManager` dedups redelivered
tasks against a bounded LRU of recently-seen task IDs rather than
re-executing them. See `specs/mqtt_telemetry.specs.md` for the full protocol.

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
  main.go                    — server wiring (HTTP mux + gRPC server + PocketBase)
  ui.go                      — SSE-driven dashboard at /
  task_mqtt.go               — MQTT task/status + presence subscriptions
  telemetry.go               — MQTT telemetry ingest wiring
  controller/
    agent_registry_http.go   — POST /agents/register (issues gRPC token + MQTT broker creds)
    agent_connect.go         — AgentService Connect handler (ListAgents)
    mission_controller.go    — POST /missions/register
    mission_connect.go       — MissionService Connect handler (ListMissions)
    task_connect.go          — TaskService Connect handler (Create/Find/List)
    telemetry_connect.go     — TelemetryService Connect handler (query/status)
  service/
    agent_registry.go        — agent auth, skills, cameras, MQTT presence
    task_dispatcher.go       — task persistence + MQTT dispatch
    mission_registry.go      — mission auth, online state, gRPC stream management
    mission_task_manager.go  — bridges mission CreateTask ↔ agent dispatch
    pose_listener.go         — pose telemetry aggregation
  repository/
    task_repo_memory.go      — in-memory TaskRepo implementation
  model/                     — Agent, Task, Mission domain types
  migrations/                — PocketBase Go migrations (config + telemetry collections)
  telemetry/                 — MQTT ingest, group schema registry, validation, Postgres persistence, live SSE feed

pkg/
  agentsdk/                  — MQTT agent SDK (Agent, TaskManager, Telemetry[T], Register)
  util/                      — shared utilities

proto/
  agent/v1/                  — internal AgentService proto (pose, transform matrices, skills — task/telemetry moved to MQTT)
  mission/v1/                — internal MissionService proto (StreamMission, CreateTask)
  api/v1/                    — external Connect API (task, agent, mission, telemetry)
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

1. Create an `agents` record in the PocketBase admin UI (`http://127.0.0.1:8090/_/`) with a unique `id`, `name`, and `key` (the device key).
2. In the agent process, use `pkg/agentsdk.Register(coreURL, deviceKey)` to exchange the device key for a gRPC token and MQTT broker credentials in one call.
3. Build an `agentsdk.NewAgent(...)`, register task handlers on `agent.TaskManager`, and call `agent.Register()` to connect over MQTT. See `cmd/mqtt-agent-example/main.go` for a complete, runnable example.
4. For pose reporting, dial gRPC with the token from step 2 and stream `AgentService.ReportPoseTelemetry`.