package controller

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"vantageos-core/internal/core/service"
	"vantageos-core/pkg/agentsdk"
)

type missionRegisterRequest struct {
	ID string `json:"id"`
}

type MissionController struct {
	mr                *service.MissionRegistry
	grpcAdvertiseAddr string
}

func NewMissionController(mr *service.MissionRegistry, gAddr string) *MissionController {
	return &MissionController{
		mr:                mr,
		grpcAdvertiseAddr: gAddr,
	}
}

func (m *MissionController) RegisterRoutes(mux *http.ServeMux) {
	slog.Info("Registering mission HTTP routes")
	mux.HandleFunc("POST /missions/register", m.handleRegister)
}

func (m *MissionController) handleRegister(w http.ResponseWriter, req *http.Request) {
	auth := req.Header.Get("Authorization")
	regToken, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || regToken == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body missionRegisterRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	id, authToken, err := m.mr.ExchangeRegToken(regToken)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := m.mr.Register(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agentsdk.RegisterResponse{
		Token:    authToken,
		GRPCAddr: m.grpcAdvertiseAddr,
	})
}
