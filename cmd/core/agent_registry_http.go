package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"vantageos-core/pkg/agentsdk"
)

type registerRequest struct {
	Skills       []AgentSkill  `json:"skills"`
	EventSources []EventSource `json:"event_sources"`
}

func (r *AgentRegistry) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /agents/register", r.handleRegister)
}

// handleRegister registers a physical agentsdk with the registry.
//
// @Summary     Register an agentsdk
// @Description Called by a physical agentsdk on boot to register its identity and skills.
// @Description The request must include a pre-shared device API key as a Bearer token.
// @Tags        agents
// @Accept      json
// @Produce     json
// @Param       Authorization  header    string           true  "Bearer <device-api-key>"
// @Param       body           body      registerRequest  true  "Agent registration payload"
// @Success     200            {object}  agentsdk.RegisterResponse
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

	token, err := r.Register(agentID, body.Skills)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agentsdk.RegisterResponse{
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
