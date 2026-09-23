package controller

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"vantageos-core/cmd/core/config"
	"vantageos-core/cmd/core/model"
	"vantageos-core/cmd/core/service"
	"vantageos-core/pkg/agentsdk"
)

type registerRequest struct {
	Skills       []model.AgentSkill      `json:"skills"`
	EventSources []model.EventSource     `json:"event_sources"`
	Cameras      []agentsdk.CameraConfig `json:"cameras"`
}

type AgentController struct {
	ar   *service.AgentRegistry
	mqtt config.MQTTConfig
}

// NewAgentController returns a controller serving /agents routes. mqttCfg is
// used only by handleRegister (spec Step 15): when mqttCfg.Enabled, a
// successful registration also mints and returns broker credentials
// alongside the existing gRPC token, leaving mqttCfg zero-valued has the
// same effect as before this feature existed -- no broker fields in the
// response.
func NewAgentController(r *service.AgentRegistry, mqttCfg config.MQTTConfig) *AgentController {
	return &AgentController{ar: r, mqtt: mqttCfg}
}

func (r *AgentController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /agents/register", r.handleRegister)
	mux.HandleFunc("GET /agents", r.handleList)
}

type agentView struct {
	ID      string                  `json:"id"`
	Name    string                  `json:"name"`
	Online  bool                    `json:"online"`
	Skills  []model.AgentSkill      `json:"skills"`
	Cameras []agentsdk.CameraConfig `json:"cameras"`
}

type listAgentsResponse struct {
	Agents []agentView `json:"agents"`
}

// handleList returns every allowed agent along with its online status,
// reported skills, and configured cameras.
//
// @Summary     List agents
// @Description Returns all provisioned agents, each annotated with whether it
// @Description currently has a live gRPC stream, plus its reported skills and cameras.
// @Tags        agents
// @Produce     json
// @Success     200  {object}  listAgentsResponse
// @Router      /agents [get]
func (r *AgentController) handleList(w http.ResponseWriter, _ *http.Request) {
	online := r.ar.OnlineAgents()

	agents := make([]agentView, 0, len(r.ar.AllowedAgents()))
	for _, allowed := range r.ar.AllowedAgents() {
		_, isOnline := online[allowed.AgentID]

		skills := r.ar.SkillsFor(allowed.AgentID)
		if skills == nil {
			skills = []model.AgentSkill{}
		}
		cameras := r.ar.GetCameras(allowed.AgentID)
		if cameras == nil {
			cameras = []agentsdk.CameraConfig{}
		}

		agents = append(agents, agentView{
			ID:      string(allowed.AgentID),
			Name:    allowed.Name,
			Online:  isOnline,
			Skills:  skills,
			Cameras: cameras,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(listAgentsResponse{Agents: agents})
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

	resp := agentsdk.RegisterResponse{
		Token:    authToken,
		GRPCAddr: r.ar.GrpcAdvertiseAddr(),
		AgentID:  id,
	}

	if r.mqtt.Enabled {
		// Re-validate the agent id as a legal topic segment before it
		// becomes a broker credential (spec Step 15) -- an agent validating
		// its own id proves nothing, and an id containing '/' or an MQTT
		// wildcard would break the fixed-depth topic layout the %u ACL
		// patterns depend on. id itself already comes from a trusted
		// PocketBase record (Step 1's ExchangeRegToken lookup), but the
		// "agents" collection's agent_id field has no such format
		// constraint enforced at the database level.
		if _, err := agentsdk.NewTopic(r.mqtt.TopicPrefix, id); err != nil {
			slog.Error("agents/register: agent id is not a valid mqtt topic segment", "agent_id", id, "err", err)
			http.Error(w, "agent id is not valid for mqtt", http.StatusInternalServerError)
			return
		}

		password, err := r.ar.IssueMQTTCredentials(agentID)
		if err != nil {
			slog.Error("agents/register: failed to issue mqtt credentials", "agent_id", id, "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		brokerURL := r.mqtt.BrokerAdvertiseURL
		if brokerURL == "" {
			brokerURL = r.mqtt.Broker
		}

		resp.BrokerURL = brokerURL
		resp.Username = id // must be the agent id -- see IssueMQTTCredentials's doc comment
		resp.Password = password
		resp.TopicPrefix = r.mqtt.TopicPrefix
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
