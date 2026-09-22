package agentsdk

import (
	"context"
	"encoding/json"
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

type TelemetryPublisher interface {
	PublishTelemetry() error
}

type Agent struct {
	ID     string `json:"agent_id"`
	Topic  Topic  `json:"-"`
	Status Status `json:"-"`

	Client mqtt.Client        `json:"-"`
	opts   mqtt.ClientOptions `json:"-"`

	mu sync.Mutex `json:"-"`

	stopTelemetryChan chan struct{} `json:"-"`

	TaskManager *TaskManager
}

func NewAgent(id string, topicPrefix string, broker string, username string, password string) *Agent {
	willPayload, err := json.Marshal(EventPayload{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Type:      EventOffline,
	})
	if err != nil {
		slog.Error("failed to build lwt payload", "error", err)
		return nil
	}

	topic := NewTopic(topicPrefix, id)

	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(id).
		SetUsername(username).
		SetPassword(password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectTimeout(10*time.Second).
		SetBinaryWill(topic.Event(), willPayload, 0, true)

	opts.OnConnect = func(c mqtt.Client) {
		slog.Info("mqtt connected", "broker", broker)
	}
	opts.OnConnectionLost = func(c mqtt.Client, err error) {
		slog.Error("mqtt connection lost", "error", err)
	}

	return &Agent{
		ID:          id,
		Topic:       topic,
		opts:        *opts,
		TaskManager: NewTaskManager(context.Background(), nil, topic.NewTask()),
	}
}

func (a *Agent) Register() {
	a.mu.Lock()
	client := mqtt.NewClient(&a.opts)
	a.Client = client
	a.mu.Unlock()

	// Connecting can block indefinitely (SetConnectRetry keeps retrying until
	// it succeeds), so do it in the background instead of blocking the caller.
	go func() {
		token := client.Connect()
		token.Wait()
		if err := token.Error(); err != nil {
			slog.Error("mqtt connect failed", "error", err)
			return
		}

		if err := a.PublishEvent(EventRegistered,  "", nil); err != nil {
			slog.Error("publish registered event failed", "error", err)
		}

		slog.Debug("Run new task listener", "id", a.ID)
		a.TaskManager.mqtt = client
		if err := a.TaskManager.RunNewTaskListener(); err != nil {
			slog.Error("new task listener failed", "error", err)
		}
	}()
}

func (a *Agent) PublishEvent(eventType EventType, reason string, payload []byte,) error {
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
	token := a.Client.Publish(a.Topic.Event(), 0, false, b)

	slog.Debug("Publishing event", "topic", a.Topic.Event(), "eventType", eventType)
	token.Wait()
	return token.Error()
}

func (a *Agent) PublishTelemetrySchema() error {
	slog.Info("PublishTelemetrySchema not implemented")

	return nil
}

func (a *Agent) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stopTelemetryChan == nil {
		return
	} else {
		close(a.stopTelemetryChan)
		a.stopTelemetryChan = nil
	}

	a.PublishEvent(EventOffline, "", nil)

	a.Client.Disconnect(1000)
	a.Client = nil
}

func (a *Agent) StartTelemetryLoop(duration time.Duration, publisher TelemetryPublisher) {
	a.mu.Lock()
	if a.stopTelemetryChan != nil {
		a.mu.Unlock()
		return
	}
	stopTelemetry := make(chan struct{})
	a.stopTelemetryChan = stopTelemetry
	a.mu.Unlock()

	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	for {
		select {
		case <-stopTelemetry:
			return
		case <-ticker.C:
			if err := publisher.PublishTelemetry(); err != nil {
				slog.Error("failed to publish telemetry", "error", err)
			}
		}
	}
}
