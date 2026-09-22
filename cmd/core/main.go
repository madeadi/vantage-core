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
	"vantageos-core/cmd/core/config"
	controller2 "vantageos-core/cmd/core/controller"
	grpc2 "vantageos-core/cmd/core/grpc"
	"vantageos-core/cmd/core/repository"
	"vantageos-core/cmd/core/service"
	_ "vantageos-core/docs"
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

	if !cfg.PocketBase.Enabled {
		slog.Error("pocketbase must be enabled: agents, missions and layouts are now sourced from PocketBase collections")
		return
	}

	pbApp, err := setupPocketBase(cfg.PocketBase)
	if err != nil {
		slog.Error("failed to start pocketbase", "err", err)
		return
	}

	// Delegate any extra args to the PocketBase root command (e.g.
	// `migrate create ...`, `migrate down`, `superuser create ...`) and exit.
	if args := flag.Args(); len(args) > 0 {
		pbApp.RootCmd.SetArgs(args)
		if err := pbApp.RootCmd.Execute(); err != nil {
			slog.Error("command failed", "err", err)
		}
		return
	}

	allowedAgents, err := loadAllowedAgents(pbApp)
	if err != nil {
		slog.Error("failed to load agents from pocketbase", "err", err)
		return
	}
	missions, err := loadMissions(pbApp)
	if err != nil {
		slog.Error("failed to load missions from pocketbase", "err", err)
		return
	}
	agentLayouts, err := loadAgentLayouts(pbApp)
	if err != nil {
		slog.Error("failed to load agent_layouts from pocketbase", "err", err)
		return
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

	// MQTT telemetry ingest is additive to the existing gRPC agent path (see
	// specs/mqtt_telemetry.specs.md) and off by default -- a core instance
	// with mqtt.enabled: false in its config runs exactly as it did before
	// this feature existed.
	if cfg.MQTT.Enabled {
		startTelemetryIngest(cfg.MQTT)
	}

	ar := service.NewAgentRegistry(allowedAgents, grpcAdvertiseAddr)
	dispatcher := service.NewTaskDispatcher(ar, tRepo)

	mr := service.NewMissionRegistry(missions)
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
	grpcSrv := grpc2.NewAgentGRPCServer(ar, telemetry, poseListener, agentLayouts, mtm, dispatcher)
	agentv1.RegisterAgentServiceServer(grpcServer, grpcSrv)

	// Re-dispatch config-collection edits into the registries without a restart,
	// then start serving the PocketBase admin UI + REST API.
	watchConfig(pbApp, ar, mr, grpcSrv)
	go servePocketBase(pbApp, cfg.PocketBase.ListenAddr)

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
