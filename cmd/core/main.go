// @title           VantageOS Core API
// @version         1.0
// @description     VantageOS backend — agentsdk registry and task management.
// @host            localhost:8080
// @BasePath        /
package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	_ "vantageos-core/docs"
	"vantageos-core/internal/core/config"
	controller2 "vantageos-core/internal/core/controller"
	grpc2 "vantageos-core/internal/core/grpc"
	"vantageos-core/internal/core/repository"
	"vantageos-core/internal/core/service"
	agentv1 "vantageos-core/proto/agent/v1"
	"vantageos-core/proto/api/v1/apiv1connect"
	missionv1 "vantageos-core/proto/mission/v1"

	httpSwagger "github.com/swaggo/http-swagger"
	"google.golang.org/grpc"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	slog.Info("Welcome to VantageOS core")

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		return
	}

	var allowedAgents []service.AllowedAgent
	for _, agt := range cfg.Agents {
		allowedAgents = append(allowedAgents, service.AllowedAgent{
			AgentID: agt.ID,
			Key:     agt.Key,
			Name:    agt.Name,
		})
	}
	grpcListenAddr := cfg.GRPCListenAddr
	if grpcListenAddr == "" {
		grpcListenAddr = ":9090"
	}
	grpcAdvertiseAddr := cfg.GRPCAdvertiseAddr
	if grpcAdvertiseAddr == "" {
		grpcAdvertiseAddr = "localhost:9090"
	}

	tRepo := repository.NewTaskRepoMemory()

	poseListener := service.NewPoseListener(1 * time.Hour)
	poseCtx, cancelPose := context.WithCancel(context.Background())
	defer cancelPose()
	go poseListener.Run(poseCtx)

	ar := service.NewAgentRegistry(allowedAgents, grpcAdvertiseAddr)
	dispatcher := service.NewTaskDispatcher(ar, tRepo)

	mr := service.NewMissionRegistry(cfg.Missions)
	mc := controller2.NewMissionController(mr, grpcAdvertiseAddr)
	ac := controller2.NewAgentController(ar)

	ui := NewUI(ar, tRepo, mr, poseListener)

	taskPath, taskHandler := apiv1connect.NewTaskServiceHandler(controller2.NewTaskConnectHandler(dispatcher))
	agentPath, agentHandler := apiv1connect.NewAgentServiceHandler(controller2.NewAgentConnectHandler(ar))
	missionPath, missionHandler := apiv1connect.NewMissionServiceHandler(controller2.NewMissionConnectHandler(mr))

	mux := http.NewServeMux()
	ac.RegisterRoutes(mux)
	ui.RegisterUIRoutes(mux)
	mc.RegisterRoutes(mux)
	mux.Handle(taskPath, taskHandler)
	mux.Handle(agentPath, agentHandler)
	mux.Handle(missionPath, missionHandler)
	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)

	// gRPC server
	lis, err := net.Listen("tcp", grpcListenAddr)
	if err != nil {
		slog.Error("failed to listen on gRPC port", "err", err)
		return
	}
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(grpc2.AuthUnaryInterceptor(ar.AuthService())),
		grpc.StreamInterceptor(combinedAuthStreamInterceptor(ar, mr)),
	)
	telemetry := service.NewTelemetryListener()

	mtm := service.NewMissionTaskManager(dispatcher, mr, tRepo)
	grpcSrv := grpc2.NewAgentGRPCServer(ar, telemetry, poseListener, cfg.AgentLayouts, mtm, dispatcher)
	agentv1.RegisterAgentServiceServer(grpcServer, grpcSrv)

	missionGrpcSrv := grpc2.NewMissionGrpc(mr, mtm)
	missionv1.RegisterMissionServiceServer(grpcServer, missionGrpcSrv)
	go func() {
		slog.Info("gRPC listening", "addr", grpcListenAddr)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("gRPC server stopped", "err", err)
		}
	}()

	slog.Info("HTTP listening", "addr", ":8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		slog.Error("server stopped", "err", err)
	}
}

// combinedAuthStreamInterceptor dispatches to the agentsdk or mission auth
// interceptor depending on which gRPC service the stream belongs to.
func combinedAuthStreamInterceptor(ar *service.AgentRegistry, mr *service.MissionRegistry) grpc.StreamServerInterceptor {
	agentAuth := grpc2.AuthStreamInterceptor(ar.AuthService())
	missionAuth := grpc2.AuthMissionStreamInterceptor(mr)
	missionServicePrefix := "/" + missionv1.MissionService_ServiceDesc.ServiceName + "/"
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if strings.HasPrefix(info.FullMethod, missionServicePrefix) {
			return missionAuth(srv, ss, info, handler)
		}
		return agentAuth(srv, ss, info, handler)
	}
}
