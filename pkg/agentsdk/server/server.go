package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"vantageos-core/pkg/agentsdk"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// tokenCreds injects the bearer token into every gRPC call's metadata.
type tokenCreds struct {
	token string
}

func (t tokenCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + t.token}, nil
}

func (t tokenCreds) RequireTransportSecurity() bool { return false }

type SkillPayload struct {
	Name string `json:"name"`
}

type Skill struct {
	Name    string       `json:"name"`
	Payload SkillPayload `json:"payload"`
}

type registerRequest struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Skills       []Skill       `json:"skills"`
	EventSources []interface{} `json:"event_sources"`
}

type Server struct {
	CoreURL string
}

type ConnectConfig struct {
	AgentID string
	Key     string
	Name    string
	Skills  []Skill
}

func (s *Server) Connect(config ConnectConfig) (*grpc.ClientConn, error) {
	body, err := json.Marshal(registerRequest{
		ID:           config.AgentID,
		Name:         config.Name,
		Skills:       config.Skills,
		EventSources: []interface{}{},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, s.CoreURL+"/agents/register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", config.Key))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registration rejected: status %d", resp.StatusCode)
	}

	var regResp agentsdk.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(
		regResp.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(tokenCreds{token: regResp.Token}),
	)
	if err != nil {
		slog.Error("failed to dial gRPC", "err", err)
		return nil, err
	}

	return conn, nil
}
