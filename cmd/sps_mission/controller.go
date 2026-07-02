package main

import (
	"encoding/json"
	"net/http"
)

// DeliveryStarter starts the mission handler registered for a delivery's type.
type DeliveryStarter interface {
	StartDelivery(delivery Delivery) error
}

type Controller struct {
	s       *DeliveryService
	starter DeliveryStarter
}

func NewController(s *DeliveryService, starter DeliveryStarter) *Controller {
	return &Controller{s: s, starter: starter}
}

func (c *Controller) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /deliveries", c.handleCreateDelivery)
	//mux.HandleFunc("GET /deliveries/{id}", c.handleFindById)
	mux.HandleFunc("/", c.home)
}

func (c *Controller) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "sps_mission is running"})
}

type createMissionRequest struct {
	ID      string       `json:"id"`
	Type    DeliveryType `json:"type"`
	RobotID string       `json:"robot_id"`
}

func (c *Controller) handleCreateDelivery(w http.ResponseWriter, r *http.Request) {
	var body createMissionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	delivery, err := c.s.Create(body.Type)
	if err != nil {
		http.Error(w, "failed to create delivery", http.StatusInternalServerError)
		return
	}

	if err := c.starter.StartDelivery(delivery); err != nil {
		http.Error(w, "failed to start delivery: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(delivery)
}
