package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type TaskHTTP struct {
	ar *AgentRegistry
}

func NewTaskHTTP(ar *AgentRegistry) *TaskHTTP {
	return &TaskHTTP{ar: ar}
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
	mux.HandleFunc("GET /api/v1/tasks", h.handleListTasks)
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

	if err := h.ar.SendTask(task); err != nil {
		slog.Error("Failed to send task to agent", "err", err)
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createTaskResponse{TaskID: taskID})
}

type taskResponse struct {
	ID        string     `json:"id"`
	AgentID   string     `json:"agent_id"`
	Type      string     `json:"type"`
	Status    TaskStatus `json:"status"`
	Payload   []byte     `json:"payload,omitempty"`
	Result    []byte     `json:"result,omitempty"`
	MissionID MissionID  `json:"mission_id,omitempty"`

	ReceivedAt string `json:"received_at,omitempty"`
	StartAt    string `json:"start_at,omitempty"`
	ToExpireAt string `json:"to_expire_at,omitempty"`
}

type listTasksResponse struct {
	Tasks []*taskResponse `json:"tasks"`
}

// handleListTasks returns tasks, optionally filtered by agent_id query param.
//
// @Summary     List tasks
// @Description Returns all tasks, optionally filtered by agent. Pass ?agent_id=<id> to filter.
// @Tags        tasks
// @Produce     json
// @Param       agent_id  query     string  false  "Filter by agent ID"
// @Success     200       {object}  listTasksResponse
// @Router      /api/v1/tasks [get]
func (h *TaskHTTP) handleListTasks(w http.ResponseWriter, req *http.Request) {
	agentID := AgentID(req.URL.Query().Get("agent_id"))

	tasks := h.ar.ListTasks(agentID)

	resp := listTasksResponse{Tasks: make([]*taskResponse, 0, len(tasks))}
	for _, t := range tasks {
		tr := &taskResponse{
			ID:        t.ID,
			AgentID:   string(t.AgentID),
			Type:      t.Type,
			Status:    t.Status,
			Payload:   t.Payload,
			Result:    t.Result,
			MissionID: t.MissionID,
		}
		if !t.ReceivedAt.IsZero() {
			tr.ReceivedAt = t.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if !t.StartAt.IsZero() {
			tr.StartAt = t.StartAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if !t.ToExpireAt.IsZero() {
			tr.ToExpireAt = t.ToExpireAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		resp.Tasks = append(resp.Tasks, tr)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func generateTaskID() (string, error) {
	return generateRandomHex(16)
}
