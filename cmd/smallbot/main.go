package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"vantageos-core/pkg/agent"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"
)

type Config struct {
	AgentID   string `yaml:"id"`
	AgentKey  string `yaml:"key"`
	AgentName string `yaml:"name"`
	CoreURL   string `yaml:"core_url"`
}

type registerRequest struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Skills       []interface{} `json:"skills"`
	EventSources []interface{} `json:"event_sources"`
}

type wsEnvelope struct {
	Type    string `json:"type"`
	Topic   string `json:"topic"`
	Payload []byte `json:"payload,omitempty"`
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func register(cfg *Config) (*agent.RegisterResponse, error) {
	body, err := json.Marshal(registerRequest{
		ID:           cfg.AgentID,
		Name:         cfg.AgentName,
		Skills:       []interface{}{},
		EventSources: []interface{}{},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, cfg.CoreURL+"/agents/register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.AgentKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registration rejected: status %d", resp.StatusCode)
	}

	var regResp agent.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return nil, err
	}
	return &regResp, nil
}

func connectWS(cfg *Config, regResp *agent.RegisterResponse) (*websocket.Conn, error) {
	coreURL, err := url.Parse(cfg.CoreURL)
	if err != nil {
		return nil, err
	}

	wsScheme := "ws"
	if coreURL.Scheme == "https" {
		wsScheme = "wss"
	}

	wsURL := url.URL{
		Scheme: wsScheme,
		Host:   coreURL.Host,
		Path:   regResp.WSURL,
		RawQuery: url.Values{
			"agent_id": {cfg.AgentID},
			"token":    {regResp.Token},
		}.Encode(),
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	return conn, err
}

func main() {
	configPath := flag.String("config", "smallbot.config.yaml", "path to config file")
	flag.Parse()

	slog.Info("Welcome to smallbot")

	cfg, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		return
	}

	regResp, err := register(cfg)
	if err != nil {
		slog.Error("registration failed", "err", err)
		return
	}
	slog.Info("registered", "agent_id", regResp.AgentID, "ws_url", regResp.WSURL)

	conn, err := connectWS(cfg, regResp)
	if err != nil {
		slog.Error("failed to connect websocket", "err", err)
		return
	}
	defer conn.Close()

	// subscribe to our task topic
	sub, _ := json.Marshal(wsEnvelope{Type: "subscribe", Topic: regResp.TopicTasks})
	if err := conn.WriteMessage(websocket.TextMessage, sub); err != nil {
		slog.Error("failed to subscribe", "err", err)
		return
	}
	slog.Info("subscribed", "topic", regResp.TopicTasks)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				slog.Error("ws read error", "err", err)
				return
			}
			var env wsEnvelope
			if err := json.Unmarshal(data, &env); err != nil {
				slog.Warn("unreadable ws message", "err", err)
				continue
			}
			if env.Type == "message" {
				slog.Info("task received", "topic", env.Topic, "payload", string(env.Payload))
			}
		}
	}()

	<-quit
	slog.Info("shutting down")
}
