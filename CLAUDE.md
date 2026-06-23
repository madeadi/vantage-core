# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./...

# Run with hot reload (requires air)
make dev-core       # runs cmd/core with core.config.yaml
make dev-smallbot   # runs cmd/smallbot with smallbot.config.yaml

# Run directly
go run ./cmd/core -config core.config.yaml
go run ./cmd/smallbot -config smallbot.config.yaml

# Regenerate Swagger docs (run after any handler change)
swag init -g cmd/core/main.go -o docs
```

## Architecture

Two binaries share a single module (`vantageos-core`):

- **`cmd/core`** — the central server. Runs an HTTP + WebSocket server on `:8080`.
- **`cmd/smallbot`** — a reference agent implementation that registers with core and receives tasks.

### Communication flow

1. Agent POSTs `Bearer <device-key>` to `POST /agents/register` → receives `{ token, ws_url, topic_tasks, topic_telemetry }`
2. Agent connects to `ws://host/ws?agent_id=<id>&token=<token>` (validated by core before upgrade)
3. Agent sends `{"type":"subscribe","topic":"agents/<id>/tasks"}` over WebSocket
4. Core publishes tasks via `{"type":"message","topic":"agents/<id>/tasks","payload":...}`
5. Agent publishes telemetry via `{"type":"publish","topic":"agents/<id>/telemetry","payload":...}`

### Key packages

- **`pkg/pubsub`** — `PubSub` interface + two implementations: `MQTTClient` (legacy, unused by core) and `WSHub` (active). `WSHub` is both the WebSocket upgrader (`ServeHTTP`) and the pub/sub router. Agents subscribe/unsubscribe/publish by sending JSON envelopes; the hub routes inbound agent messages to core-side handlers registered via `Subscribe()`.
- **`pkg/agent`** — shared types between core and agents (currently just `RegisterResponse`).

### Inside `cmd/core`

- `AgentRegistry` — tracks allowed agents (pre-shared keys), online agents, tokens, and skills. Also implements `Onliner` for the task manager.
- `TaskManager` — publishes to `agents/<agentID>/tasks` via the `Publisher` interface. Blocks if the agent is offline or already busy.
- `TelemetryListener` — subscribes to `agents/<agentID>/telemetry` per agent via `Watch(agentID)`.
- `/ws` route in `main.go` wraps `hub.ServeHTTP` with token auth before the WebSocket upgrade.

### Swagger

Handlers use `swaggo/swag` annotations. After changing any handler, run `swag init` (see command above) to regenerate `docs/`. The spec is served at `/swagger/`.
