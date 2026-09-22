package agentsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

type Status string

const (
	StatusRegistered Status = "registered"
)

// errNoClient is returned by operations that need a connected mqtt client
// before one has been attached (see Agent.Register, TaskManager.SetClient).
var errNoClient = errors.New("agentsdk: not connected")

type Agent struct {
	ID     string `json:"agent_id"`
	Topic  Topic  `json:"-"`
	Status Status `json:"-"`

	Client mqtt.Client        `json:"-"`
	opts   mqtt.ClientOptions `json:"-"`

	mu             sync.Mutex `json:"-"`
	connectedOnce  bool       `json:"-"` // guards the one-time registered event + task listener setup
	onConnectFuncs []func()   `json:"-"`

	TaskManager *TaskManager
}

// NewAgent builds an Agent for id, using CleanSession=false so the broker
// keeps its subscriptions (and any queued QoS-1+ messages) across a dropped
// connection and automatic reconnect — see SetAutoReconnect/SetConnectRetry
// below, and specs/mqtt_telemetry.specs.md's task-dispatch notes on why a
// persistent session is preferable to resubscribing by hand on every
// reconnect.
func NewAgent(id string, topicPrefix string, broker string, username string, password string) *Agent {
	topic, err := NewTopic(topicPrefix, id)
	if err != nil {
		slog.Error("invalid agent topic", "error", err)
		return nil
	}

	willPayload, err := json.Marshal(EventPayload{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Type:      EventOffline,
	})
	if err != nil {
		slog.Error("failed to build lwt payload", "error", err)
		return nil
	}

	a := &Agent{
		ID:          id,
		Topic:       topic,
		TaskManager: NewTaskManager(context.Background(), topic.NewTask()),
	}

	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(id).
		SetUsername(username).
		SetPassword(password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectTimeout(10*time.Second).
		SetCleanSession(false).
		SetBinaryWill(topic.Event(), willPayload, 1, true)

	opts.OnConnect = func(mqtt.Client) {
		a.handleConnect()
	}
	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		slog.Error("mqtt connection lost", "agent_id", a.ID, "error", err)
	}

	a.opts = *opts
	return a
}

// OnConnect registers fn to run every time the agent's connection becomes
// active — the first connect and every automatic reconnect after a drop.
// Call it before Register to also run fn on the first connect; a call after
// Register may race the first connect and miss it. NewTelemetry uses this to
// keep an agent's advisory schema announcement current after a broker
// restart clears its retained state.
func (a *Agent) OnConnect(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onConnectFuncs = append(a.onConnectFuncs, fn)
}

// handleConnect runs on every connect and reconnect. It publishes the
// "online" event every time, and — only the first time this agent instance
// connects — publishes "registered" and starts the task listener. With
// CleanSession=false (see NewAgent), the broker keeps that subscription
// across later reconnects, so it does not need to be re-established.
func (a *Agent) handleConnect() {
	slog.Info("mqtt connected", "agent_id", a.ID)

	if err := a.PublishEvent(EventOnline, "", nil); err != nil {
		slog.Error("publish online event failed", "agent_id", a.ID, "error", err)
	}

	a.mu.Lock()
	first := !a.connectedOnce
	a.connectedOnce = true
	hooks := append([]func(){}, a.onConnectFuncs...)
	a.mu.Unlock()

	if first {
		if err := a.PublishEvent(EventRegistered, "", nil); err != nil {
			slog.Error("publish registered event failed", "agent_id", a.ID, "error", err)
		}

		a.TaskManager.SetClient(a.mqttClient())
		if err := a.TaskManager.RunNewTaskListener(); err != nil {
			slog.Error("new task listener failed", "agent_id", a.ID, "error", err)
		}
	}

	for _, fn := range hooks {
		fn()
	}
}

// mqttClient returns the agent's current mqtt client, or nil if Register has
// not been called yet.
func (a *Agent) mqttClient() mqtt.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Client
}

// Register connects the agent in the background and returns immediately —
// connecting can block indefinitely, since SetConnectRetry keeps retrying
// until it succeeds. Everything that needs to happen once connected
// (lifecycle events, the task listener, any OnConnect hooks) runs from
// handleConnect, which fires on this first connect and on every
// automatic reconnect after a dropped connection.
func (a *Agent) Register() {
	a.mu.Lock()
	client := mqtt.NewClient(&a.opts)
	a.Client = client
	a.mu.Unlock()

	go func() {
		token := client.Connect()
		token.Wait()
		if err := token.Error(); err != nil {
			slog.Error("mqtt connect failed", "agent_id", a.ID, "error", err)
		}
	}()
}

// PublishEvent publishes an agent lifecycle event (see EventType), retained
// so a subscriber that connects after the fact still sees the agent's most
// recent state.
func (a *Agent) PublishEvent(eventType EventType, reason string, payload []byte) error {
	client := a.mqttClient()
	if client == nil {
		return fmt.Errorf("agentsdk: PublishEvent: agent %q: %w", a.ID, errNoClient)
	}

	b, err := json.Marshal(EventPayload{
		ID:        uuid.New().String(),
		AgentID:   a.ID,
		Timestamp: time.Now(),
		Type:      eventType,
		Reason:    reason,
		Payload:   payload,
	})
	if err != nil {
		return err
	}

	token := client.Publish(a.Topic.Event(), 1, true, b)
	slog.Debug("publishing event", "agent_id", a.ID, "topic", a.Topic.Event(), "event_type", eventType)
	token.Wait()
	return token.Error()
}

// Stop publishes an offline event and disconnects, unconditionally — a
// never-connected agent (Client is nil) is a no-op rather than an error,
// since there is nothing to publish to or disconnect from.
func (a *Agent) Stop() {
	if a.mqttClient() == nil {
		return
	}

	if err := a.PublishEvent(EventOffline, "", nil); err != nil {
		slog.Error("publish offline event failed", "agent_id", a.ID, "error", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Client != nil {
		a.Client.Disconnect(1000)
		a.Client = nil
	}
}
