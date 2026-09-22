// Package ingest subscribes to every agent's telemetry and telemetry-schema
// topics and fans ingested messages out to registered listeners (schema
// registry, validator, persistence, the live SSE feed — added in later
// steps). See specs/mqtt_telemetry.specs.md Step 6.
package ingest

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"vantageos-core/pkg/agentsdk"
)

// Kind distinguishes which topic a Message arrived on.
type Kind int

const (
	KindTelemetry Kind = iota
	KindTelemetrySchema
)

func (k Kind) String() string {
	switch k {
	case KindTelemetry:
		return "telemetry"
	case KindTelemetrySchema:
		return "telemetry_schema"
	default:
		return "unknown"
	}
}

// Message is one ingested MQTT publish, already resolved to the agent ID
// that published it.
type Message struct {
	AgentID    string
	Kind       Kind
	Payload    []byte
	ReceivedAt time.Time
}

// Stats is a snapshot of the daemon's counters.
type Stats struct {
	Ingested   uint64
	Dropped    uint64
	QueueDepth int
}

// Listener is called, from the daemon's single consumer goroutine (Run),
// for every ingested Message. It must not block: Run does not read the next
// queued message until every listener for the current one has returned, so
// a slow listener stalls every other listener and, eventually, ingestion
// itself. A listener that does real work should hand off to its own
// goroutine or queue rather than doing that work inline.
type Listener func(Message)

// Daemon subscribes to every agent's telemetry and telemetry-schema topics
// under a prefix and fans ingested messages out to registered Listeners.
//
// The mqtt.MessageHandler callbacks Subscribe registers run on paho's own
// single dispatch goroutine — its own docs: "callback must not block or call
// functions within this package that may block" — so they do nothing but a
// non-blocking enqueue. Blocking there would stall delivery of every other
// subscribed topic on the same connection, including the presence-carrying
// event topic a later step subscribes to on it. A single goroutine (Run)
// drains the queue; at this project's 10-agent/1Hz scale target (see
// specs/mqtt_telemetry.specs.md) a worker pool buys nothing.
type Daemon struct {
	prefix string
	queue  chan Message

	mu        sync.Mutex
	listeners []Listener

	ingested atomic.Uint64
	dropped  atomic.Uint64
}

// New returns a Daemon subscribing under prefix, with a queue sized for
// bufferSize messages of headroom — sized so a brief downstream stall (e.g.
// a database write) is absorbed rather than dropped, not to accommodate
// sustained overload at a higher scale than this project currently targets.
func New(prefix string, bufferSize int) *Daemon {
	return &Daemon{
		prefix: prefix,
		queue:  make(chan Message, bufferSize),
	}
}

// AddListener registers fn to be called for every ingested Message. Call it
// before Run starts consuming; registering a listener concurrently with an
// in-progress dispatch in Run is not safe.
func (d *Daemon) AddListener(fn Listener) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listeners = append(d.listeners, fn)
}

// Subscribe subscribes an already-connected client to every agent's
// telemetry and telemetry-schema topics under the daemon's prefix. Call it
// again after every reconnect unless client uses a persistent session
// (CleanSession=false) that keeps the subscription server-side.
func (d *Daemon) Subscribe(client mqtt.Client) error {
	token := client.Subscribe(agentsdk.TelemetryFilter(d.prefix), 0, d.handle(KindTelemetry))
	token.Wait()
	if err := token.Error(); err != nil {
		return err
	}

	token = client.Subscribe(agentsdk.TelemetrySchemaFilter(d.prefix), 1, d.handle(KindTelemetrySchema))
	token.Wait()
	return token.Error()
}

// handle returns the mqtt.MessageHandler for kind. This is the only code
// that runs on paho's dispatch goroutine — see the Daemon doc comment for
// why it must stay non-blocking.
func (d *Daemon) handle(kind Kind) mqtt.MessageHandler {
	return func(_ mqtt.Client, msg mqtt.Message) {
		agentID, ok := agentsdk.AgentIDFromTopic(d.prefix, msg.Topic())
		if !ok {
			slog.Warn("ingest: topic did not match the agent-id-first layout", "topic", msg.Topic())
			return
		}

		// Copy the payload: nothing in paho's own docs guarantees the slice
		// Message.Payload() returns stays valid, or unmodified, once this
		// handler returns, and the cost is negligible at this message size.
		payload := append([]byte(nil), msg.Payload()...)

		m := Message{
			AgentID:    agentID,
			Kind:       kind,
			Payload:    payload,
			ReceivedAt: time.Now(),
		}

		select {
		case d.queue <- m:
		default:
			d.dropped.Add(1)
			slog.Warn("ingest: queue full, dropping message",
				"agent_id", agentID, "kind", kind, "queue_depth", len(d.queue))
		}
	}
}

// Run drains the queue and fans each Message out to every registered
// Listener, until ctx is done. It blocks — run it in its own goroutine.
func (d *Daemon) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-d.queue:
			d.ingested.Add(1)

			d.mu.Lock()
			listeners := d.listeners
			d.mu.Unlock()

			for _, fn := range listeners {
				fn(m)
			}
		}
	}
}

// Stats returns a snapshot of the daemon's counters.
func (d *Daemon) Stats() Stats {
	return Stats{
		Ingested:   d.ingested.Load(),
		Dropped:    d.dropped.Load(),
		QueueDepth: len(d.queue),
	}
}
