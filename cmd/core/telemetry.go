package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"vantageos-core/cmd/core/config"
	"vantageos-core/cmd/core/model"
	"vantageos-core/cmd/core/service"
	"vantageos-core/cmd/core/telemetry/events"
	"vantageos-core/cmd/core/telemetry/ingest"
	"vantageos-core/cmd/core/telemetry/live"
	"vantageos-core/cmd/core/telemetry/mapping"
	"vantageos-core/cmd/core/telemetry/persistcfg"
	"vantageos-core/cmd/core/telemetry/registry"
	"vantageos-core/cmd/core/telemetry/store"
	"vantageos-core/cmd/core/telemetry/validate"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/pocketbase/pocketbase/core"
)

// ingestQueueSize is sized for ~100s of headroom at this project's
// 10-agent/1Hz scale target (see specs/mqtt_telemetry.specs.md), not for
// sustained overload at a materially higher rate.
const ingestQueueSize = 1024

// startTelemetryIngest connects core's own mqtt client and starts the
// telemetry ingest daemon (cmd/core/telemetry/ingest) in the background,
// wiring its output through validate.Validator and events.Throttler to
// agent_events (via events.PocketBaseSink), and -- when persistStore is
// non-nil -- through mapping.Apply into the Postgres/TimescaleDB store,
// gated per-group by persistRegistry (Step 12's "on/off" requirement).
// persistStore/persistRegistry are both nil when persistence is disabled
// (cfg.Telemetry.PersistenceEnabled false): validation and throttling still
// run exactly the same, only the store.Enqueue call is skipped, matching
// Step 12's "Done when": toggling persistence off stops writes without
// disabling validation.
//
// liveBroadcaster (Step 13), when non-nil, is published to for every
// KindTelemetry message regardless of validity or group membership -- the
// live view exists precisely so an agent developer with no group/contract
// configured yet can still see what they're sending.
//
// dispatcher/ar/tuHandler wire Step 16's task dispatch over MQTT onto this
// same connection: dispatcher.SetMQTTClient lets SendTask publish task/new
// for agents with no gRPC stream, and subscribeTaskControlPlane (see
// cmd/core/task_mqtt.go) subscribes task/status (acks) and event
// (presence). All three are optional -- nil/zero skips that wiring,
// letting callers that only care about telemetry (e.g. this function's own
// tests) opt out of the rest.
//
// Every ingested telemetry message also refreshes ar's MQTT presence
// signal (when ar is non-nil) as a bonus liveness indicator alongside the
// explicit online/offline events subscribeTaskControlPlane handles -- see
// AgentRegistry.MarkMQTTOnline's doc comment on why this is a bonus, not
// the primary signal.
//
// Connection failures are logged, not fatal: MQTT telemetry is additive to
// the existing gRPC agent path, so a broker that's down or misconfigured
// must not take the rest of core down with it. SetAutoReconnect and
// SetConnectRetry mean the client keeps trying rather than giving up after
// the first failure, and OnConnect re-subscribes on every connect (including
// a reconnect), which is a harmless no-op if the subscription is already
// live server-side.
func startTelemetryIngest(
	cfg config.MQTTConfig, telemetryCfg config.TelemetryConfig, app core.App,
	schemaRegistry *registry.Registry, persistRegistry *persistcfg.Registry, persistStore *store.Store,
	liveBroadcaster *live.Broadcaster,
	dispatcher *service.TaskDispatcher, ar *service.AgentRegistry, tuHandler taskUpdatedHandler,
) *ingest.Daemon {
	daemon := ingest.New(cfg.TopicPrefix, ingestQueueSize)

	validator := validate.New(schemaRegistry)
	throttler := events.New(events.NewPocketBaseSink(app), events.Config{Window: telemetryCfg.ThrottleWindow})

	daemon.AddListener(func(m ingest.Message) {
		var violation *validate.Violation
		switch m.Kind {
		case ingest.KindTelemetry:
			if ar != nil {
				ar.MarkMQTTOnline(model.AgentID(m.AgentID))
			}
			violation = validator.Validate(m.AgentID, m.Payload)
			if persistStore != nil {
				enqueueTelemetryRow(persistStore, persistRegistry, schemaRegistry, throttler, m, violation)
			}
			if liveBroadcaster != nil {
				publishLiveEnvelope(liveBroadcaster, m, violation)
			}
		case ingest.KindTelemetrySchema:
			violation = validator.CheckSchemaAnnouncement(m.AgentID, m.Payload)
		default:
			return
		}
		if violation != nil {
			throttler.Record(*violation)
		}
	})

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectTimeout(10 * time.Second)

	opts.OnConnect = func(client mqtt.Client) {
		slog.Info("telemetry ingest: mqtt connected", "broker", cfg.Broker)
		if err := daemon.Subscribe(client); err != nil {
			slog.Error("telemetry ingest: subscribe failed", "error", err)
		}
		if dispatcher != nil {
			dispatcher.SetMQTTClient(client, cfg.TopicPrefix)
		}
		if ar != nil && tuHandler != nil {
			subscribeTaskControlPlane(client, cfg.TopicPrefix, ar, tuHandler)
		}
	}
	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		slog.Error("telemetry ingest: mqtt connection lost", "error", err)
	}

	client := mqtt.NewClient(opts)
	go func() {
		token := client.Connect()
		token.Wait()
		if err := token.Error(); err != nil {
			slog.Error("telemetry ingest: initial connect failed, will keep retrying", "error", err)
		}
	}()

	go daemon.Run(context.Background())

	return daemon
}

// enqueueTelemetryRow resolves m's group (independent of whether that group
// has a validation contract -- a no_contract group can still be persisted),
// applies its telemetry_mapping (Step 11), and -- only if the group's
// persist_enabled toggle is on -- enqueues the resulting row into
// persistStore. validationResult is the *validate.Violation the caller
// already computed for m (or nil), reused here rather than validating the
// same payload against the same schema a second time. A mapping.Apply
// bad_timestamp violation is recorded through throttler exactly like a
// validation violation, since it flows into the same agent_events path.
func enqueueTelemetryRow(persistStore *store.Store, persistRegistry *persistcfg.Registry, schemaRegistry *registry.Registry, throttler *events.Throttler, m ingest.Message, validationResult *validate.Violation) {
	groupID, hasGroup := schemaRegistry.GroupID(m.AgentID)
	if !hasGroup || !persistRegistry.PersistEnabled(groupID) {
		return
	}

	// Resolve's ok is ignored -- entry.Hash is "" (its zero value) in the
	// no_contract case, which is exactly the SchemaHash a no_contract Row
	// should carry.
	entry, _ := schemaRegistry.Resolve(m.AgentID)
	valid := validPtr(validationResult)

	spec := persistRegistry.Mapping(groupID)
	result, mappingViolation := mapping.Apply(spec, m.AgentID, groupID, m.ReceivedAt, m.Payload)
	if mappingViolation != nil {
		throttler.Record(*mappingViolation)
	}

	var fields json.RawMessage
	if len(result.Fields) > 0 {
		b, err := json.Marshal(result.Fields)
		if err != nil {
			slog.Error("telemetry persist: marshal mapped fields failed", "agent_id", m.AgentID, "err", err)
		} else {
			fields = b
		}
	}

	persistStore.Enqueue(store.Row{
		Time:       result.Time,
		ReceivedAt: result.ReceivedAt,
		AgentID:    result.AgentID,
		GroupID:    groupID,
		SchemaHash: entry.Hash,
		Valid:      valid,
		Fields:     fields,
		Payload:    m.Payload,
	})
}

// validPtr converts a *validate.Violation (as Validate returns it -- see
// that method's doc comment) into the tri-state store.Row.Valid/live
// envelope representation: nil for no_contract (no contract to validate
// against), else &true/&false. Shared by enqueueTelemetryRow and
// publishLiveEnvelope so both -- persistence and the live view -- agree on
// what "valid" means for the same violation.
func validPtr(v *validate.Violation) *bool {
	switch {
	case v == nil:
		t := true
		return &t
	case v.Kind == validate.KindNoContract:
		return nil
	default:
		f := false
		return &f
	}
}

// liveEnvelope is what publishLiveEnvelope actually broadcasts -- the raw
// telemetry payload plus the same validity Step 12 persists, so
// ui/src/pages/telemetry/Live.tsx (Step 14) can mark each row valid/invalid
// without re-validating client-side (which would need to fetch and compile
// the group's JSON Schema in the browser). Valid is omitted (nil) for
// no_contract, matching store.Row.Valid's tri-state.
type liveEnvelope struct {
	Valid   *bool           `json:"valid,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

func publishLiveEnvelope(b *live.Broadcaster, m ingest.Message, violation *validate.Violation) {
	env, err := json.Marshal(liveEnvelope{Valid: validPtr(violation), Payload: m.Payload})
	if err != nil {
		slog.Error("telemetry live: marshal envelope failed", "agent_id", m.AgentID, "err", err)
		return
	}
	b.Publish(m.AgentID, env)
}
