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
```

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

### Key packages

- **`pkg/agentsdk`** — agent-side SDK: skill runner, service manager, task dispatcher used by agent binaries.
- **`pkg/pubsub`** — legacy WebSocket pub/sub hub (unused by core, kept for reference).
- **`pkg/util`** — shared utilities.

### Inside `cmd/core`

- `AgentRegistry` — tracks allowed agents (pre-shared keys), online agents, tokens, skills, and gRPC streams. Implements `Reconnector` to re-dispatch active tasks on reconnect.
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