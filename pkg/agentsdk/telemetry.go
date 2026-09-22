package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// Telemetry publishes values of type T as an agent's telemetry, and T's
// derived schema as the agent's advisory schema announcement (see
// DeriveTelemetrySchema). Deriving the schema from T rather than
// hand-authoring it means the declared shape and the published data cannot
// drift apart by construction.
//
// The wire payload for Publish is T marshaled directly, with no envelope —
// agents publish raw, self-shaped payloads (see specs/mqtt_telemetry.specs.md).
type Telemetry[T any] struct {
	agent  *Agent
	schema TelemetrySchema
	seq    atomic.Uint64
}

// NewTelemetry derives T's schema once and returns a Telemetry ready to
// publish values of T for agent. It also registers a hook (via
// agent.OnConnect) that re-announces the schema on every connect and
// reconnect, so the retained announcement survives a broker restart that
// clears its retained-message store. Call NewTelemetry before agent.Register
// so that hook is in place for the first connect too.
func NewTelemetry[T any](agent *Agent) (*Telemetry[T], error) {
	if agent == nil {
		return nil, fmt.Errorf("agentsdk: NewTelemetry: agent must not be nil")
	}
	schema, err := DeriveTelemetrySchema[T]()
	if err != nil {
		return nil, err
	}

	t := &Telemetry[T]{agent: agent, schema: schema}
	agent.OnConnect(func() {
		if err := t.PublishSchema(); err != nil {
			slog.Error("failed to publish telemetry schema", "agent_id", agent.ID, "error", err)
		}
	})
	return t, nil
}

// SchemaJSON returns T's derived JSON Schema, as announced by PublishSchema.
func (t *Telemetry[T]) SchemaJSON() []byte { return t.schema.Schema }

// SchemaHash returns the stable content hash of T's derived schema.
func (t *Telemetry[T]) SchemaHash() string { return t.schema.Hash }

// PublishSchema announces T's derived schema (advisory — see
// DeriveTelemetrySchema) on the agent's telemetry-schema topic, retained so
// a late-connecting or restarted daemon still sees it on subscribe.
func (t *Telemetry[T]) PublishSchema() error {
	client := t.agent.mqttClient()
	if client == nil {
		return fmt.Errorf("agentsdk: PublishSchema: agent %q: %w", t.agent.ID, errNoClient)
	}

	body, err := json.Marshal(TelemetrySchemaAnnouncement{
		AgentID:    t.agent.ID,
		Timestamp:  time.Now(),
		SchemaHash: t.schema.Hash,
		Schema:     t.schema.Schema,
	})
	if err != nil {
		return fmt.Errorf("agentsdk: marshal schema announcement: %w", err)
	}

	token := client.Publish(t.agent.Topic.TelemetrySchema(), 1, true, body)
	token.Wait()
	return token.Error()
}

// Publish marshals v as-is and publishes it on the agent's telemetry topic.
func (t *Telemetry[T]) Publish(v T) error {
	client := t.agent.mqttClient()
	if client == nil {
		return fmt.Errorf("agentsdk: Publish: agent %q: %w", t.agent.ID, errNoClient)
	}

	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("agentsdk: marshal telemetry: %w", err)
	}

	token := client.Publish(t.agent.Topic.Telemetry(), 0, false, body)
	token.Wait()
	if err := token.Error(); err != nil {
		return err
	}

	seq := t.seq.Add(1)
	slog.Debug("published telemetry", "agent_id", t.agent.ID, "seq", seq)
	return nil
}

// StartLoop calls src on every tick of every and publishes the result, until
// ctx is done. It spawns a goroutine and returns immediately — unlike
// Agent.StartTelemetryLoop, it never blocks its caller. Errors from src or
// Publish are logged and the loop continues; there is no per-agent failure
// state for a caller to escalate here, since one failed tick does not mean
// the connection is gone (the mqtt client's own reconnect logic handles
// that) or that the next tick will fail too.
func (t *Telemetry[T]) StartLoop(ctx context.Context, every time.Duration, src func(context.Context) (T, error)) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				v, err := src(ctx)
				if err != nil {
					slog.Error("telemetry source failed", "agent_id", t.agent.ID, "error", err)
					continue
				}
				if err := t.Publish(v); err != nil {
					slog.Error("failed to publish telemetry", "agent_id", t.agent.ID, "error", err)
				}
			}
		}
	}()
}
