package controller

import (
	"context"

	"connectrpc.com/connect"

	"vantageos-core/internal/core/model"
	"vantageos-core/internal/core/service"
	apiv1 "vantageos-core/proto/api/v1"
	"vantageos-core/proto/api/v1/apiv1connect"
)

type MissionConnectHandler struct {
	apiv1connect.UnimplementedMissionServiceHandler
	mr *service.MissionRegistry
}

var _ apiv1connect.MissionServiceHandler = (*MissionConnectHandler)(nil)

func NewMissionConnectHandler(mr *service.MissionRegistry) *MissionConnectHandler {
	return &MissionConnectHandler{mr: mr}
}

func (h *MissionConnectHandler) ListMissions(_ context.Context, _ *connect.Request[apiv1.ListMissionsRequest]) (*connect.Response[apiv1.ListMissionsResponse], error) {
	infos := h.mr.ListMissions()
	missions := make([]*apiv1.Mission, len(infos))
	for i, m := range infos {
		missions[i] = &apiv1.Mission{
			Id:     m.ID,
			Name:   m.Name,
			Online: m.Online,
			Status: missionStatusToProto(m.Status),
		}
	}
	return connect.NewResponse(&apiv1.ListMissionsResponse{Missions: missions}), nil
}

func missionStatusToProto(s model.MissionStatus) apiv1.MissionStatus {
	switch s {
	case model.MissionStatusPending:
		return apiv1.MissionStatus_MISSION_STATUS_PENDING
	case model.MissionStatusAccepted:
		return apiv1.MissionStatus_MISSION_STATUS_ACCEPTED
	case model.MissionStatusRejected:
		return apiv1.MissionStatus_MISSION_STATUS_REJECTED
	case model.MissionStatusRunning:
		return apiv1.MissionStatus_MISSION_STATUS_RUNNING
	case model.MissionStatusCompleted:
		return apiv1.MissionStatus_MISSION_STATUS_COMPLETED
	case model.MissionStatusFailed:
		return apiv1.MissionStatus_MISSION_STATUS_FAILED
	default:
		return apiv1.MissionStatus_MISSION_STATUS_UNSPECIFIED
	}
}
