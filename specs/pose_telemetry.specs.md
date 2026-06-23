# Pose Telemetry Broadcast

Agents should be able to publish their current pose (position + heading) and core should parse and store it.

## Payload format

Telemetry messages are typed. Pose uses the existing `agents/<id>/telemetry` topic with a JSON body:

```json
{
  "type": "pose",
  "x": 1.23,
  "y": 4.56,
  "heading": 0.78
}
```

- `x`, `y` — position in meters, world frame
- `heading` — orientation in radians, counter-clockwise from +X axis

## Agent side (smallbot)

Smallbot publishes pose periodically by sending a `publish` envelope over the existing WebSocket connection:

```
loop every N seconds (e.g. 1 Hz):
    payload = { "type": "pose", "x": ..., "y": ..., "heading": ... }
    send { "type": "publish", "topic": regResp.TopicTelemetry, "payload": payload }
```

Pose values in smallbot are simulated (static or incrementing).

## Core side (TelemetryListener)

`TelemetryListener.Watch` already subscribes to `agents/<id>/telemetry`. Extend the handler to parse the type field and dispatch:

```
on telemetry message:
    unmarshal outer envelope → get "type"
    if type == "pose":
        unmarshal into Pose{X, Y, Heading}
        store in AgentRegistry (or a dedicated PoseStore) keyed by agentID
        log at debug level
    else:
        log raw payload (current behaviour)
```

## Storage

`AgentRegistry` gains a `SetPose(agentID, Pose)` / `GetPose(agentID) (Pose, bool)` pair backed by an in-memory map. No persistence required for now.

A new HTTP endpoint exposes the latest pose:

```
GET /agents/{id}/pose
→ 200 { "x": 1.23, "y": 4.56, "heading": 0.78 }
→ 404 if agent unknown or no pose received yet
```

## Considerations

- Pose is overwritten on each message — only latest is kept. History is out of scope.
- No schema versioning needed yet; `type` field is sufficient for future extension.
- Heading unit (radians) matches OpenRMF convention from `rmf_yaml_research.specs.md`.
- Rate-limiting on core is not needed at 1 Hz per agent for current scale.
