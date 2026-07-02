package controller

import (
	"context"
	"encoding/json"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	"vantageos-core/internal/core/service"
	apiv1 "vantageos-core/proto/api/v1"
	"vantageos-core/proto/api/v1/apiv1connect"
)

type AgentConnectHandler struct {
	apiv1connect.UnimplementedAgentServiceHandler
	ar *service.AgentRegistry
}

var _ apiv1connect.AgentServiceHandler = (*AgentConnectHandler)(nil)

func NewAgentConnectHandler(ar *service.AgentRegistry) *AgentConnectHandler {
	return &AgentConnectHandler{ar: ar}
}

func (h *AgentConnectHandler) ListAgents(_ context.Context, _ *connect.Request[apiv1.ListAgentsRequest]) (*connect.Response[apiv1.ListAgentsResponse], error) {
	online := h.ar.OnlineAgents()

	var agents []*apiv1.Agent
	for _, allowed := range h.ar.AllowedAgents() {
		id := string(allowed.AgentID)
		_, isOnline := online[allowed.AgentID]

		pt := &apiv1.Agent{
			Id:     id,
			Name:   allowed.Name,
			Online: isOnline,
		}

		for _, s := range h.ar.SkillsFor(allowed.AgentID) {
			ps := &apiv1.AgentSkill{Name: s.Name}
			if len(s.Payload) > 0 {
				var m map[string]any
				if err := json.Unmarshal(s.Payload, &m); err == nil {
					ps.Payload, _ = structpb.NewStruct(m)
				}
			}
			pt.Skills = append(pt.Skills, ps)
		}

		for _, c := range h.ar.GetCameras(allowed.AgentID) {
			pt.Cameras = append(pt.Cameras, &apiv1.Camera{
				CameraId: c.CameraID,
				Type:     cameraTypeToProto(string(c.Type)),
				Url:      c.Url,
			})
		}

		agents = append(agents, pt)
	}

	return connect.NewResponse(&apiv1.ListAgentsResponse{Agents: agents}), nil
}

func cameraTypeToProto(t string) apiv1.CameraType {
	switch t {
	case "mjpg":
		return apiv1.CameraType_CAMERA_TYPE_MJPG
	case "rtsp":
		return apiv1.CameraType_CAMERA_TYPE_RTSP
	case "webrtc":
		return apiv1.CameraType_CAMERA_TYPE_WEBRTC
	default:
		return apiv1.CameraType_CAMERA_TYPE_UNSPECIFIED
	}
}
