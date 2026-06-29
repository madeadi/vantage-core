package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type TaskHTTP struct {
	tm *TaskManager
}

type createTaskRequest struct {
	AgentID string          `json:"agent_id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload" swaggertype:"object"`
}

type createTaskResponse struct {
	TaskID string `json:"task_id"`
}

func (h *TaskHTTP) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tasks", h.handleCreateTask)
}

// handleCreateTask dispatches a task to an online agentsdk.
//
// @Summary     Create a task
// @Description Dispatches a task to a named agentsdk. The agentsdk must be online and idle.
// @Tags        tasks
// @Accept      json
// @Produce     json
// @Param       body  body      createTaskRequest   true  "Task payload"
// @Success     200   {object}  createTaskResponse
// @Failure     400   {string}  string  "bad request"
// @Failure     409   {string}  string  "agentsdk offline or busy"
// @Failure     500   {string}  string  "internal server error"
// @Router      /api/v1/tasks [post]
func (h *TaskHTTP) handleCreateTask(w http.ResponseWriter, req *http.Request) {
	var body createTaskRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.AgentID == "" || body.Type == "" {
		http.Error(w, "robotId and type are required", http.StatusBadRequest)
		return
	}

	taskID, err := generateTaskID()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	task := &Task{
		ID:      taskID,
		AgentID: AgentID(body.AgentID),
		Type:    body.Type,
		Payload: []byte(body.Payload),
		Status:  TaskStatusDraft,
	}

	if err := h.tm.StartTask(task); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createTaskResponse{TaskID: taskID})
}

func generateTaskID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
