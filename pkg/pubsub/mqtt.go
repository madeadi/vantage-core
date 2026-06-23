package pubsub

import (
	"fmt"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MQTTConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	ClientID  string
	Namespace string
}

type MQTTClient struct {
	client    mqtt.Client
	mu        sync.RWMutex
	handlers  map[string]func(string, []byte)
	namespace string
}

func NewMQTTClient(cfg MQTTConfig) (*MQTTClient, error) {
	mc := &MQTTClient{
		handlers:  make(map[string]func(string, []byte)),
		namespace: cfg.Namespace,
	}

	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port)).
		SetClientID(cfg.ClientID).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetDefaultPublishHandler(mc.defaultHandler)

	mc.client = mqtt.NewClient(opts)
	if token := mc.client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	return mc, nil
}

func (mc *MQTTClient) defaultHandler(_ mqtt.Client, msg mqtt.Message) {
	mc.mu.RLock()
	h, ok := mc.handlers[msg.Topic()]
	mc.mu.RUnlock()
	if ok {
		h(msg.Topic(), msg.Payload())
	}
}

func (mc *MQTTClient) fullTopic(topic string) string {
	return mc.namespace + "/" + topic
}

func (mc *MQTTClient) Publish(topic string, payload []byte) error {
	token := mc.client.Publish(mc.fullTopic(topic), 1, false, payload)
	token.Wait()
	return token.Error()
}

func (mc *MQTTClient) Subscribe(topic string, handler func(string, []byte)) error {
	mc.mu.Lock()
	mc.handlers[mc.fullTopic(topic)] = handler
	mc.mu.Unlock()

	token := mc.client.Subscribe(mc.fullTopic(topic), 1, nil)
	token.Wait()
	return token.Error()
}

func (mc *MQTTClient) Unsubscribe(topic string) error {
	mc.mu.Lock()
	delete(mc.handlers, mc.fullTopic(topic))
	mc.mu.Unlock()

	token := mc.client.Unsubscribe(mc.namespace + "/" + topic)
	token.Wait()
	return token.Error()
}

func (mc *MQTTClient) Close() error {
	mc.client.Disconnect(250)
	return nil
}
