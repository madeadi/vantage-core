package pubsub

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type wsEnvelope struct {
	Type    string `json:"type"`
	Topic   string `json:"topic"`
	Payload []byte `json:"payload,omitempty"`
}

type wsClient struct {
	hub    *WSHub
	conn   *websocket.Conn
	send   chan []byte
	topics map[string]bool
	mu     sync.Mutex
}

type WSHub struct {
	clients   map[*wsClient]struct{}
	topicSubs map[string]map[*wsClient]struct{}
	handlers  map[string]func(string, []byte)
	mu        sync.RWMutex
	upgrader  websocket.Upgrader
}

func NewWSHub() *WSHub {
	return &WSHub{
		clients:   make(map[*wsClient]struct{}),
		topicSubs: make(map[string]map[*wsClient]struct{}),
		handlers:  make(map[string]func(string, []byte)),
		upgrader:  websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}
}

// ServeHTTP handles the WebSocket upgrade — register with your HTTP mux.
func (h *WSHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "err", err)
		return
	}
	c := &wsClient{
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, 64),
		topics: make(map[string]bool),
	}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go c.writePump()
	go c.readPump()
}

// Publish sends payload to all agent clients subscribed to topic.
func (h *WSHub) Publish(topic string, payload []byte) error {
	msg, _ := json.Marshal(wsEnvelope{Type: "message", Topic: topic, Payload: payload})
	h.mu.RLock()
	subs := h.topicSubs[topic]
	h.mu.RUnlock()
	for c := range subs {
		select {
		case c.send <- msg:
		default:
			slog.Warn("ws client send buffer full, dropping message", "topic", topic)
		}
	}
	return nil
}

// Subscribe registers a core-side handler for messages agents publish on topic.
func (h *WSHub) Subscribe(topic string, handler func(string, []byte)) error {
	h.mu.Lock()
	h.handlers[topic] = handler
	h.mu.Unlock()
	return nil
}

// Unsubscribe removes the core-side handler for topic.
func (h *WSHub) Unsubscribe(topic string) error {
	h.mu.Lock()
	delete(h.handlers, topic)
	h.mu.Unlock()
	return nil
}

func (h *WSHub) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.conn.Close()
	}
	return nil
}

func (h *WSHub) dispatch(topic string, payload []byte) {
	h.mu.RLock()
	handler, ok := h.handlers[topic]
	h.mu.RUnlock()
	if ok {
		handler(topic, payload)
	}
}

func (h *WSHub) addClientTopic(c *wsClient, topic string) {
	h.mu.Lock()
	if h.topicSubs[topic] == nil {
		h.topicSubs[topic] = make(map[*wsClient]struct{})
	}
	h.topicSubs[topic][c] = struct{}{}
	h.mu.Unlock()

	c.mu.Lock()
	c.topics[topic] = true
	c.mu.Unlock()
}

func (h *WSHub) removeClientTopic(c *wsClient, topic string) {
	h.mu.Lock()
	delete(h.topicSubs[topic], c)
	h.mu.Unlock()

	c.mu.Lock()
	delete(c.topics, topic)
	c.mu.Unlock()
}

func (h *WSHub) removeClient(c *wsClient) {
	h.mu.Lock()
	c.mu.Lock()
	for topic := range c.topics {
		delete(h.topicSubs[topic], c)
	}
	c.mu.Unlock()
	delete(h.clients, c)
	h.mu.Unlock()
	close(c.send)
}

func (c *wsClient) readPump() {
	defer func() {
		c.hub.removeClient(c)
		c.conn.Close()
	}()
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var env wsEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.Type {
		case "subscribe":
			c.hub.addClientTopic(c, env.Topic)
		case "unsubscribe":
			c.hub.removeClientTopic(c, env.Topic)
		case "publish":
			c.hub.dispatch(env.Topic, env.Payload)
		}
	}
}

func (c *wsClient) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}
