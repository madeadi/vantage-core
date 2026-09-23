// @title           VantageOS Core API
// @version         1.0
// @description     VantageOS backend — agentsdk registry and task management.
// @host            localhost:8321
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
	"vantageos-core/cmd/core/telemetry/live"
	"vantageos-core/cmd/core/telemetry/persistcfg"
	"vantageos-core/cmd/core/telemetry/query"
	"vantageos-core/cmd/core/telemetry/registry"
	"vantageos-core/cmd/core/telemetry/store"
	_ "vantageos-core/docs"
	agentv1 "vantageos-core/proto/agent/v1"
	"vantageos-core/proto/api/v1/apiv1connect"
	missionv1 "vantageos-core/proto/mission/v1"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
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
	agentGroupSchemas, err := loadAgentGroupSchemas(pbApp)
	if err != nil {
		slog.Error("failed to load agent_groups from pocketbase", "err", err)
		return
	}
	agentGroupMemberships, err := loadAgentGroupMemberships(pbApp)
	if err != nil {
		slog.Error("failed to load agent group memberships from pocketbase", "err", err)
		return
	}
	schemaRegistry := registry.New()
	schemaRegistry.SetAgentGroups(agentGroupMemberships)
	if err := schemaRegistry.SetGroupSchemas(agentGroupSchemas); err != nil {
		// Not fatal: SetGroupSchemas still installs every schema that did
		// compile -- see its doc comment.
		slog.Error("one or more agent_groups schemas failed to compile", "err", err)
	}

	// persistRegistry stays nil (and watchConfig/startTelemetryIngest treat
	// that as "persistence is off") unless cfg.Telemetry.PersistenceEnabled --
	// no point tracking telemetry_mapping/telemetry_settings reloads for a
	// store that will never read them.
	var persistRegistry *persistcfg.Registry
	var persistStore *store.Store
	// telemetryQuerier stays nil under the same condition as persistRegistry
	// -- TelemetryConnectHandler treats a nil querier as "persistence off"
	// and degrades QueryTelemetry/status counts accordingly rather than
	// panicking (see that handler's doc comment).
	var telemetryQuerier *query.Querier
	if cfg.Telemetry.PersistenceEnabled {
		persistRegistry = persistcfg.New()
		agentGroupMappings, err := loadAgentGroupMappings(pbApp)
		if err != nil {
			slog.Error("failed to load agent_groups telemetry_mapping from pocketbase", "err", err)
			return
		}
		persistRegistry.SetMappings(agentGroupMappings)
		telemetrySettings, err := loadTelemetryPersistSettings(pbApp)
		if err != nil {
			slog.Error("failed to load telemetry_settings from pocketbase", "err", err)
			return
		}
		persistRegistry.SetPersistEnabled(telemetrySettings)

		pgPool, err := pgxpool.New(context.Background(), cfg.Telemetry.DSN)
		if err != nil {
			slog.Error("telemetry persistence: failed to create postgres pool", "err", err)
			return
		}
		if err := store.Migrate(context.Background(), pgPool); err != nil {
			slog.Error("telemetry persistence: migration failed", "err", err)
			return
		}
		if err := store.ApplyRetentionPolicy(context.Background(), pgPool, cfg.Telemetry.Retention); err != nil {
			// Not fatal: the migration already declared a 90-day policy (see
			// migrations/0001_init.sql), so a failure here just means a
			// non-default cfg.Telemetry.Retention wasn't applied -- ingestion
			// itself is unaffected.
			slog.Error("telemetry persistence: failed to apply retention policy", "err", err)
		}

		persistStore = store.New(pgPool, store.Config{
			BatchSize:     cfg.Telemetry.BatchSize,
			FlushInterval: cfg.Telemetry.FlushInterval,
		})
		go persistStore.Run(context.Background())
		telemetryQuerier = query.New(pgPool)
		slog.Info("telemetry persistence enabled", "batch_size", cfg.Telemetry.BatchSize, "flush_interval", cfg.Telemetry.FlushInterval)
	}

	// liveBroadcaster (Step 13) is always constructed, even with MQTT
	// disabled -- the SSE endpoint and ConnectRPC handler exist regardless,
	// they just never receive anything to publish until ingest is running.
	liveBroadcaster := live.New()

	httpListenAddr := cfg.HTTPListenAddr
	if httpListenAddr == "" {
		httpListenAddr = ":8080"
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

	// Constructed here (rather than after the MQTT block, as in earlier
	// steps) because startTelemetryIngest also wires Step 16's task
	// dispatch/presence onto the same MQTT connection, and needs all three.
	ar := service.NewAgentRegistry(allowedAgents, grpcAdvertiseAddr)
	dispatcher := service.NewTaskDispatcher(tRepo)
	mr := service.NewMissionRegistry(missions)
	mtm := service.NewMissionTaskManager(dispatcher, mr, tRepo)

	// MQTT telemetry ingest, task dispatch and presence (specs/mqtt_telemetry.specs.md
	// Steps 6/16) are off by default -- a core instance with mqtt.enabled:
	// false in its config has no task transport and no telemetry ingest at
	// all (the gRPC ReportTelemetry/StreamTasks RPCs that used to provide a
	// fallback were removed in Step 17).
	if cfg.MQTT.Enabled {
		startTelemetryIngest(cfg.MQTT, cfg.Telemetry, pbApp, schemaRegistry, persistRegistry, persistStore, liveBroadcaster, dispatcher, ar, mtm)
	}

	mc := controller2.NewMissionController(mr, grpcAdvertiseAddr)
	ac := controller2.NewAgentController(ar, cfg.MQTT)

	ui := NewUI(ar, tRepo, mr, poseListener)

	taskPath, taskHandler := apiv1connect.NewTaskServiceHandler(controller2.NewTaskConnectHandler(dispatcher))
	agentPath, agentHandler := apiv1connect.NewAgentServiceHandler(controller2.NewAgentConnectHandler(ar))
	missionPath, missionHandler := apiv1connect.NewMissionServiceHandler(controller2.NewMissionConnectHandler(mr))
	telemetryPath, telemetryHandler := apiv1connect.NewTelemetryServiceHandler(
		controller2.NewTelemetryConnectHandler(pbApp, schemaRegistry, telemetryQuerier),
		connect.WithInterceptors(controller2.RequireOperatorInterceptor(pbApp)),
	)
	telemetryLive := controller2.NewTelemetryLiveController(pbApp, liveBroadcaster)

	mux := http.NewServeMux()
	ac.RegisterRoutes(mux)
	ui.RegisterUIRoutes(mux)
	mc.RegisterRoutes(mux)
	telemetryLive.RegisterRoutes(mux)
	mux.Handle(taskPath, taskHandler)
	mux.Handle(agentPath, agentHandler)
	mux.Handle(missionPath, missionHandler)
	mux.Handle(telemetryPath, telemetryHandler)
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

	grpcSrv := grpc2.NewAgentGRPCServer(ar, poseListener, agentLayouts)
	agentv1.RegisterAgentServiceServer(grpcServer, grpcSrv)

	// Re-dispatch config-collection edits into the registries without a restart,
	// then start serving the PocketBase admin UI + REST API.
	watchConfig(pbApp, ar, mr, grpcSrv, schemaRegistry, persistRegistry)
	go servePocketBase(pbApp, cfg.PocketBase.ListenAddr)

	missionGrpcSrv := grpc2.NewMissionGrpc(mr, mtm)
	missionv1.RegisterMissionServiceServer(grpcServer, missionGrpcSrv)
	go func() {
		slog.Info("gRPC listening", "addr", grpcListenAddr)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("gRPC server stopped", "err", err)
		}
	}()

	slog.Info("HTTP listening", "addr", httpListenAddr)
	if err := http.ListenAndServe(httpListenAddr, mux); err != nil {
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
