package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"vantageos-core/pkg/agent"
)

type registerRequest struct {
	ID           AgentID       `json:"id"`
	Name         string        `json:"name"`
	Skills       []AgentSkill  `json:"skills"`
	EventSources []EventSource `json:"event_sources"`
}

func (r *AgentRegistry) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /agents/register", r.handleRegister)
}

// handleRegister registers a physical agent with the registry.
//
// @Summary     Register an agent
// @Description Called by a physical agent on boot to register its identity and skills.
// @Description The request must include a pre-shared device API key as a Bearer token.
// @Tags        agents
// @Accept      json
// @Produce     json
// @Param       Authorization  header    string           true  "Bearer <device-api-key>"
// @Param       body           body      registerRequest  true  "Agent registration payload"
// @Success     200            {object}  agent.RegisterResponse
// @Failure     400            {string}  string  "bad request"
// @Failure     401            {string}  string  "unauthorized"
// @Failure     500            {string}  string  "internal server error"
// @Router      /agents/register [post]
func (r *AgentRegistry) handleRegister(w http.ResponseWriter, req *http.Request) {
	agentID, ok := r.authenticateDeviceKey(req)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body registerRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if body.ID == "" || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// device key must match the agent ID in the body
	if body.ID != agentID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	token, err := generateToken()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	r.Register(&Agent{ID: body.ID, Name: body.Name}, token, body.Skills)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agent.RegisterResponse{
		AgentID:  string(agentID),
		Token:    token,
		GRPCAddr: r.grpcAdvertiseAddr,
	})
}

func (r *AgentRegistry) authenticateDeviceKey(req *http.Request) (AgentID, bool) {
	auth := req.Header.Get("Authorization")
	key, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || key == "" {
		return "", false
	}
	agentID, ok := r.getAgentIDByKey(key)
	if !ok {
		return "", false
	}
	return agentID, ok
}

func (r *AgentRegistry) getAgentIDByKey(key string) (AgentID, bool) {
	for _, allowedAgent := range r.allowedAgents {
		if allowedAgent.Key == key {
			return allowedAgent.AgentID, true
		}
	}
	return "", false
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
