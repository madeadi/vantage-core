# WebSocket Reconnect

Smallbot currently connects once and exits if the connection drops. Needs a retry loop.

## Idea

Wrap the connect + subscribe + read loop in a `for` loop with a backoff sleep on failure.

```
for {
    connect → if fail, sleep 5s, retry
    subscribe to tasks topic
    read loop until error
    // on disconnect: loop back and reconnect
}
```

## Considerations

- Token is issued at registration and reused on reconnect — fine unless core restarts (tokens are in-memory)
- If core restarts, smallbot needs to re-register before reconnecting
- Could solve this by re-running the full register → connect sequence on any failure
- Backoff should be capped (e.g. 5s → 10s → 30s) to avoid hammering core on outage
