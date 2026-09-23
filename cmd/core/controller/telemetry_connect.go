package controller

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"vantageos-core/cmd/core/telemetry/events"
	"vantageos-core/cmd/core/telemetry/query"
	"vantageos-core/cmd/core/telemetry/registry"
	apiv1 "vantageos-core/proto/api/v1"
	"vantageos-core/proto/api/v1/apiv1connect"
)

// defaultStatusWindow matches GetTelemetryStatusRequest.window's documented
// default in proto/api/v1/telemetry.proto.
const defaultStatusWindow = time.Hour

// recentViolationsLimit bounds how many agent_events rows GetTelemetryStatus
// returns -- a status check, not a full violation history browser.
const recentViolationsLimit = 20

// TelemetryConnectHandler implements apiv1connect.TelemetryServiceHandler
// (Step 13). querier is nil when persistence is disabled
// (cfg.Telemetry.PersistenceEnabled false) -- QueryTelemetry and the
// count/last-seen part of GetTelemetryStatus degrade to
// Unavailable/zero-valued rather than panicking, since validation (and so
// "is my schema right") must keep working independent of persistence.
type TelemetryConnectHandler struct {
	apiv1connect.UnimplementedTelemetryServiceHandler
	app      core.App
	registry *registry.Registry
	querier  *query.Querier
}

var _ apiv1connect.TelemetryServiceHandler = (*TelemetryConnectHandler)(nil)

// NewTelemetryConnectHandler returns a handler answering against app (for
// agent_events/recent violations), schemaRegistry (for an agent's current
// group/contract hash), and querier (for stored points/counts -- may be nil,
// see the type's doc comment).
func NewTelemetryConnectHandler(app core.App, schemaRegistry *registry.Registry, querier *query.Querier) *TelemetryConnectHandler {
	return &TelemetryConnectHandler{app: app, registry: schemaRegistry, querier: querier}
}

func (h *TelemetryConnectHandler) QueryTelemetry(ctx context.Context, req *connect.Request[apiv1.QueryTelemetryRequest]) (*connect.Response[apiv1.QueryTelemetryResponse], error) {
	msg := req.Msg
	if msg.AgentId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent_id is required"))
	}
	if h.querier == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("telemetry persistence is disabled on this core instance"))
	}

	to := time.Now().UTC()
	if msg.To != nil {
		to = msg.To.AsTime()
	}
	from := to.Add(-time.Hour)
	if msg.From != nil {
		from = msg.From.AsTime()
	}

	points, nextToken, err := h.querier.Query(ctx, msg.AgentId, from, to, msg.Bucket, int(msg.Limit), msg.PageToken)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	pts := make([]*apiv1.TelemetryPoint, len(points))
	for i, p := range points {
		pts[i] = pointToProto(p)
	}
	return connect.NewResponse(&apiv1.QueryTelemetryResponse{Points: pts, NextPageToken: nextToken}), nil
}

func (h *TelemetryConnectHandler) GetTelemetryStatus(ctx context.Context, req *connect.Request[apiv1.GetTelemetryStatusRequest]) (*connect.Response[apiv1.GetTelemetryStatusResponse], error) {
	msg := req.Msg
	if msg.AgentId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent_id is required"))
	}
	window := defaultStatusWindow
	if msg.Window != "" {
		d, err := time.ParseDuration(msg.Window)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid window: "+err.Error()))
		}
		window = d
	}

	resp := &apiv1.GetTelemetryStatusResponse{AgentId: msg.AgentId}
	if entry, ok := h.registry.Resolve(msg.AgentId); ok {
		resp.GroupId = entry.GroupID
		resp.SchemaHash = entry.Hash
	} else if groupID, ok := h.registry.GroupID(msg.AgentId); ok {
		resp.GroupId = groupID // has a group, just no contract set (no_contract)
	}

	if h.querier != nil {
		lastSeen, validCount, invalidCount, err := h.querier.Status(ctx, msg.AgentId, window)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if !lastSeen.IsZero() {
			resp.LastSeen = timestamppb.New(lastSeen)
		}
		resp.ValidCount = int32(validCount)
		resp.InvalidCount = int32(invalidCount)
	}

	violations, err := h.recentViolations(msg.AgentId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp.RecentViolations = violations

	return connect.NewResponse(resp), nil
}

func (h *TelemetryConnectHandler) recentViolations(agentID string) ([]*apiv1.TelemetryViolationSummary, error) {
	records, err := h.app.FindRecordsByFilter(
		events.AgentEventsCollection,
		"agent_id = {:agentId} && type = {:type}",
		"-last_seen",
		recentViolationsLimit,
		0,
		map[string]any{"agentId": agentID, "type": events.TypeTelemetryViolation},
	)
	if err != nil {
		return nil, err
	}

	out := make([]*apiv1.TelemetryViolationSummary, len(records))
	for i, r := range records {
		out[i] = &apiv1.TelemetryViolationSummary{
			Kind:       r.GetString("kind"),
			Detail:     r.GetString("detail"),
			Count:      int32(r.GetInt("count")),
			LastSeen:   timestamppb.New(r.GetDateTime("last_seen").Time()),
			Paths:      violationPaths(r),
			SampleJson: violationSampleJSON(r),
		}
	}
	return out, nil
}

// violationPaths decodes agent_events.paths (a JSONField -- see
// cmd/core/migrations/1788900003_add_paths_to_agent_events.go) back into a
// string slice. Absent for a violation Kind with no specific path (e.g.
// no_contract), or for a row written before that migration existed.
func violationPaths(r *core.Record) []string {
	var paths []string
	if err := r.UnmarshalJSONField("paths", &paths); err != nil {
		return nil
	}
	return paths
}

// violationSampleJSON returns agent_events.sample's raw JSON text verbatim
// -- see TelemetryViolationSummary.sample_json's doc comment on why this
// isn't decoded into a Struct.
func violationSampleJSON(r *core.Record) string {
	raw, ok := r.Get("sample").(types.JSONRaw)
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return string(raw)
}

func pointToProto(p query.Point) *apiv1.TelemetryPoint {
	pt := &apiv1.TelemetryPoint{
		AgentId:    p.AgentID,
		Time:       timestamppb.New(p.Time),
		ReceivedAt: timestamppb.New(p.ReceivedAt),
		GroupId:    p.GroupID,
		SchemaHash: p.SchemaHash,
		Payload:    jsonToStruct(p.Payload),
	}
	if p.Valid != nil {
		pt.Valid = wrapperspb.Bool(*p.Valid)
	}
	if len(p.Fields) > 0 {
		pt.Fields = jsonToStruct(p.Fields)
	}
	return pt
}

// jsonToStruct decodes raw jsonb bytes (already valid JSON -- store.go only
// ever writes json.Marshal output or the original ingested payload) into a
// Struct for the wire response. Returns nil on malformed input rather than
// erroring the whole response -- unlike bytesToStruct (task_connect.go),
// which treats malformed input as legitimately absent, a telemetry payload's
// bytes came from an agent and might genuinely be bad; nil here just drops
// that one field from the response instead of failing the query.
func jsonToStruct(b []byte) *structpb.Struct {
	if len(b) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		return nil
	}
	return s
}
