// Package live fans out ingested telemetry payloads to per-agent live
// subscribers -- Step 13's GET /telemetry/live?agent_id= SSE endpoint, fed
// from Step 6's ingest fan-out (cmd/core/telemetry/ingest.Daemon.AddListener).
// See specs/mqtt_telemetry.specs.md Step 13.
package live

import "sync"

// Broadcaster fans out telemetry payloads to per-agent subscribers. Safe for
// concurrent use.
type Broadcaster struct {
	mu   sync.Mutex
	subs map[string]map[chan []byte]struct{} // agent_id -> set of subscriber channels
}

// New returns an empty Broadcaster.
func New() *Broadcaster {
	return &Broadcaster{subs: make(map[string]map[chan []byte]struct{})}
}

// Subscribe returns a channel that receives the payload of every telemetry
// message Publish'd for agentID from now on, until unsubscribe is called.
// unsubscribe must be called exactly once (typically via defer) or the
// subscription -- and its channel -- leaks.
func (b *Broadcaster) Subscribe(agentID string) (ch <-chan []byte, unsubscribe func()) {
	c := make(chan []byte, 1) // capacity 1: a live view only ever wants the latest value, see Publish
	b.mu.Lock()
	if b.subs[agentID] == nil {
		b.subs[agentID] = make(map[chan []byte]struct{})
	}
	b.subs[agentID][c] = struct{}{}
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		delete(b.subs[agentID], c)
		if len(b.subs[agentID]) == 0 {
			delete(b.subs, agentID)
		}
		b.mu.Unlock()
	}
	return c, unsub
}

// Publish delivers payload to every current subscriber of agentID.
// Non-blocking: a subscriber whose channel is still full from a previous
// update it hasn't read yet has that stale update dropped in favor of this
// newer one, rather than Publish blocking on it -- called from the ingest
// daemon's own single consumer goroutine (see ingest.Listener's doc comment
// on why listeners must never block), and a live view only cares about the
// current value, not a guaranteed-delivery history (that's what
// QueryTelemetry/replay is for).
func (b *Broadcaster) Publish(agentID string, payload []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.subs[agentID] {
		select {
		case c <- payload:
		default:
			select {
			case <-c: // drop the stale, unread update
			default:
			}
			select {
			case c <- payload:
			default: // a concurrent reader refilled it between our drain and this send; drop, next Publish will get through
			}
		}
	}
}
