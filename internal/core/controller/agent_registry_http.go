package controller

import (
	"encoding/json"
	"net/http"
	"strings"
	"vantageos-core/internal/core/model"
	"vantageos-core/internal/core/service"
	"vantageos-core/pkg/agentsdk"
)

type registerRequest struct {
	Skills       []model.AgentSkill      `json:"skills"`
	EventSources []model.EventSource     `json:"event_sources"`
	Cameras      []agentsdk.CameraConfig `json:"cameras"`
}

type AgentController struct {
	ar *service.AgentRegistry
}

func NewAgentController(r *service.AgentRegistry) *AgentController {
	return &AgentController{ar: r}
}

func (r *AgentController) RegisterRoutes(mux *http.ServeMux) {
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
func (r *AgentController) handleRegister(w http.ResponseWriter, req *http.Request) {
	auth := req.Header.Get("Authorization")
	regToken, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || regToken == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, authToken, err := r.ar.ExchangeRegToken(regToken)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	agentID := model.AgentID(id)

	var body registerRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	r.ar.Register(agentID, body.Skills, body.Cameras)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agentsdk.RegisterResponse{
		Token:    authToken,
		GRPCAddr: r.ar.GrpcAdvertiseAddr(),
	})
}
