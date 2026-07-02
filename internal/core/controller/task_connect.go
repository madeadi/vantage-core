package controller

import (
	"context"
	"encoding/json"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"vantageos-core/internal/core/model"
	"vantageos-core/internal/core/service"
	apiv1 "vantageos-core/proto/api/v1"
	"vantageos-core/proto/api/v1/apiv1connect"
)

type TaskConnectHandler struct {
	apiv1connect.UnimplementedTaskServiceHandler
	dispatcher *service.TaskDispatcher
}

var _ apiv1connect.TaskServiceHandler = (*TaskConnectHandler)(nil)

func NewTaskConnectHandler(dispatcher *service.TaskDispatcher) *TaskConnectHandler {
	return &TaskConnectHandler{dispatcher: dispatcher}
}

func (h *TaskConnectHandler) CreateTask(ctx context.Context, req *connect.Request[apiv1.CreateTaskRequest]) (*connect.Response[apiv1.CreateTaskResponse], error) {
	msg := req.Msg
	if msg.AgentId == "" || msg.Type == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent_id and type are required"))
	}

	payload, err := structToBytes(msg.Payload)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid payload"))
	}

	task := &model.Task{
		ID:      uuid.New().String(),
		AgentID: model.AgentID(msg.AgentId),
		Type:    msg.Type,
		Payload: payload,
		Status:  model.TaskStatusDraft,
	}
	if msg.ToExpireAt != nil {
		task.ToExpireAt = msg.ToExpireAt.AsTime()
	}

	if err := h.dispatcher.SendTask(task); err != nil {
		switch err.Error() {
		case "agent is busy":
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		case "no active stream for agent":
			return nil, connect.NewError(connect.CodeUnavailable, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&apiv1.CreateTaskResponse{TaskId: task.ID}), nil
}

func (h *TaskConnectHandler) FindTask(ctx context.Context, req *connect.Request[apiv1.FindTaskRequest]) (*connect.Response[apiv1.FindTaskResponse], error) {
	task, err := h.dispatcher.FindTask(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if task == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("task not found"))
	}
	return connect.NewResponse(&apiv1.FindTaskResponse{Task: taskModelToProto(task)}), nil
}

func (h *TaskConnectHandler) ListTasks(ctx context.Context, req *connect.Request[apiv1.ListTasksRequest]) (*connect.Response[apiv1.ListTasksResponse], error) {
	tasks := h.dispatcher.ListTasks(model.AgentID(req.Msg.AgentId))
	pts := make([]*apiv1.Task, len(tasks))
	for i, t := range tasks {
		pts[i] = taskModelToProto(t)
	}
	return connect.NewResponse(&apiv1.ListTasksResponse{Tasks: pts}), nil
}

func taskModelToProto(t *model.Task) *apiv1.Task {
	pt := &apiv1.Task{
		Id:               t.ID,
		AgentId:          string(t.AgentID),
		Type:             t.Type,
		Payload:          bytesToStruct(t.Payload),
		Result:           bytesToStruct(t.Result),
		Status:           apiv1.TaskStatus(t.Status),
		MissionId:        t.MissionID,
		MissionContextId: t.MissionContextID,
		MissionContext:   t.MissionContext,
	}
	if !t.ReceivedAt.IsZero() {
		pt.ReceivedAt = timestamppb.New(t.ReceivedAt)
	}
	if !t.StartAt.IsZero() {
		pt.StartAt = timestamppb.New(t.StartAt)
	}
	if !t.ToExpireAt.IsZero() {
		pt.ToExpireAt = timestamppb.New(t.ToExpireAt)
	}
	return pt
}

// structToBytes marshals a Struct to its JSON bytes for storage in model.Task.
func structToBytes(s *structpb.Struct) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s.AsMap())
}

// bytesToStruct unmarshals JSON bytes from model.Task back into a Struct.
// Returns nil if the bytes are empty or invalid — callers treat nil as absent.
func bytesToStruct(b []byte) *structpb.Struct {
	if len(b) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	s, _ := structpb.NewStruct(m)
	return s
}
