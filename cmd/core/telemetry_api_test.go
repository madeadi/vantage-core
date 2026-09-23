package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/pocketbase/pocketbase/core"

	controller2 "vantageos-core/cmd/core/controller"
	"vantageos-core/cmd/core/telemetry/events"
	"vantageos-core/cmd/core/telemetry/live"
	"vantageos-core/cmd/core/telemetry/query"
	"vantageos-core/cmd/core/telemetry/registry"
	"vantageos-core/cmd/core/telemetry/validate"
	apiv1 "vantageos-core/proto/api/v1"
	"vantageos-core/proto/api/v1/apiv1connect"
)

// mustCreateUser creates a users record with the given role and returns a
// fresh auth token for it (core.Record.NewAuthToken -- the same kind of
// token PocketBase's own login endpoint would hand back, just minted
// directly rather than over HTTP).
func mustCreateUser(t *testing.T, app core.App, username, role string) string {
	t.Helper()
	coll, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	r := core.NewRecord(coll)
	r.Set("username", username)
	r.Set("email", username+"@example.com")
	r.Set("role", role)
	r.Set("verified", true)
	r.SetPassword("test-password-1234")
	if err := app.Save(r); err != nil {
		t.Fatalf("save user %q: %v", username, err)
	}
	token, err := r.NewAuthToken()
	if err != nil {
		t.Fatalf("NewAuthToken for %q: %v", username, err)
	}
	return token
}

// TestTelemetryAPIAuth is Step 13's "Auth via the PocketBase token; operator
// reads" -- a request with no token, or a viewer-role token, is rejected; an
// operator-role token succeeds. Exercises the real RequireOperatorInterceptor
// wired exactly as main() wires it, over a real httptest server.
func TestTelemetryAPIAuth(t *testing.T) {
	app := bootstrapTestApp(t)
	pool := telemetryTestPool(t)

	group := mustCreateGroup(t, app, "Warehouse Fleet", batteryTestSchema)
	mustCreateAgent(t, app, "api-test-agent", group.Id)
	operatorToken := mustCreateUser(t, app, "op-user", "operator")
	viewerToken := mustCreateUser(t, app, "view-user", "viewer")

	schemas, err := loadAgentGroupSchemas(app)
	if err != nil {
		t.Fatal(err)
	}
	memberships, err := loadAgentGroupMemberships(app)
	if err != nil {
		t.Fatal(err)
	}
	schemaRegistry := registry.New()
	schemaRegistry.SetAgentGroups(memberships)
	if err := schemaRegistry.SetGroupSchemas(schemas); err != nil {
		t.Fatal(err)
	}

	querier := query.New(pool)
	handler := controller2.NewTelemetryConnectHandler(app, schemaRegistry, querier)
	path, connectHandler := apiv1connect.NewTelemetryServiceHandler(handler,
		connect.WithInterceptors(controller2.RequireOperatorInterceptor(app)))

	mux := http.NewServeMux()
	mux.Handle(path, connectHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := apiv1connect.NewTelemetryServiceClient(server.Client(), server.URL)

	t.Run("no token is rejected", func(t *testing.T) {
		req := connect.NewRequest(&apiv1.GetTelemetryStatusRequest{AgentId: "api-test-agent"})
		_, err := client.GetTelemetryStatus(context.Background(), req)
		if err == nil {
			t.Fatal("want an error with no Authorization header, got nil")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("code = %v, want Unauthenticated", connect.CodeOf(err))
		}
	})

	t.Run("viewer role is rejected", func(t *testing.T) {
		req := connect.NewRequest(&apiv1.GetTelemetryStatusRequest{AgentId: "api-test-agent"})
		req.Header().Set("Authorization", "Bearer "+viewerToken)
		_, err := client.GetTelemetryStatus(context.Background(), req)
		if err == nil {
			t.Fatal("want an error for a viewer-role token, got nil")
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("code = %v, want Unauthenticated", connect.CodeOf(err))
		}
	})

	t.Run("operator role succeeds", func(t *testing.T) {
		req := connect.NewRequest(&apiv1.GetTelemetryStatusRequest{AgentId: "api-test-agent"})
		req.Header().Set("Authorization", "Bearer "+operatorToken)
		resp, err := client.GetTelemetryStatus(context.Background(), req)
		if err != nil {
			t.Fatalf("operator request failed: %v", err)
		}
		if resp.Msg.GroupId != group.Id {
			t.Errorf("group_id = %q, want %q", resp.Msg.GroupId, group.Id)
		}
		if resp.Msg.SchemaHash == "" {
			t.Error("schema_hash is empty, want the group's compiled contract hash")
		}
	})
}

// TestGetTelemetryStatusRecentViolationsIncludesPathsAndSample proves
// agent_events.paths (added by
// cmd/core/migrations/1788900003_add_paths_to_agent_events.go) and .sample
// round-trip through GetTelemetryStatus -- Step 14's UI needs the JSON
// Pointer path shown beside the offending sample payload.
func TestGetTelemetryStatusRecentViolationsIncludesPathsAndSample(t *testing.T) {
	app := bootstrapTestApp(t)
	operatorToken := mustCreateUser(t, app, "op-user", "operator")

	coll, err := app.FindCollectionByNameOrId(events.AgentEventsCollection)
	if err != nil {
		t.Fatalf("find agent_events: %v", err)
	}
	r := core.NewRecord(coll)
	r.Set("agent_id", "violation-test-agent")
	r.Set("type", events.TypeTelemetryViolation)
	r.Set("severity", "warning")
	r.Set("kind", string(validate.KindMissingRequired))
	r.Set("signature", "sig-1")
	r.Set("detail", "missing required field(s): battery_percent")
	r.Set("paths", []string{"/battery_percent"})
	r.Set("sample", json.RawMessage(`{"status":"idle"}`))
	r.Set("count", 4)
	r.Set("first_seen", time.Now().Add(-time.Minute))
	r.Set("last_seen", time.Now())
	if err := app.Save(r); err != nil {
		t.Fatalf("save agent_events row: %v", err)
	}

	handler := controller2.NewTelemetryConnectHandler(app, registry.New(), nil)
	path, connectHandler := apiv1connect.NewTelemetryServiceHandler(handler,
		connect.WithInterceptors(controller2.RequireOperatorInterceptor(app)))
	mux := http.NewServeMux()
	mux.Handle(path, connectHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := apiv1connect.NewTelemetryServiceClient(server.Client(), server.URL)

	req := connect.NewRequest(&apiv1.GetTelemetryStatusRequest{AgentId: "violation-test-agent"})
	req.Header().Set("Authorization", "Bearer "+operatorToken)
	resp, err := client.GetTelemetryStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("GetTelemetryStatus: %v", err)
	}
	if len(resp.Msg.RecentViolations) != 1 {
		t.Fatalf("got %d recent violations, want 1", len(resp.Msg.RecentViolations))
	}
	v := resp.Msg.RecentViolations[0]
	if len(v.Paths) != 1 || v.Paths[0] != "/battery_percent" {
		t.Errorf("paths = %v, want [/battery_percent]", v.Paths)
	}
	if v.SampleJson != `{"status":"idle"}` {
		t.Errorf("sample_json = %q, want {\"status\":\"idle\"}", v.SampleJson)
	}
	if v.Count != 4 {
		t.Errorf("count = %d, want 4", v.Count)
	}
}

// TestQueryTelemetryReturnsStoredPoints is Step 13's "query" requirement,
// end-to-end against real Postgres: rows inserted the way store.Store would
// write them come back correctly through the ConnectRPC handler.
func TestQueryTelemetryReturnsStoredPoints(t *testing.T) {
	app := bootstrapTestApp(t)
	pool := telemetryTestPool(t)

	group := mustCreateGroup(t, app, "Warehouse Fleet", batteryTestSchema)
	mustCreateAgent(t, app, "query-test-agent", group.Id)
	operatorToken := mustCreateUser(t, app, "op-user", "operator")

	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO telemetry (time, received_at, agent_id, group_id, schema_hash, valid, fields, payload)
		 VALUES ($1, $1, $2, $3, 'hash-x', true, NULL, $4)`,
		now, "query-test-agent", group.Id, `{"battery_percent": 77}`,
	); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	schemaRegistry := registry.New()
	querier := query.New(pool)
	handler := controller2.NewTelemetryConnectHandler(app, schemaRegistry, querier)
	path, connectHandler := apiv1connect.NewTelemetryServiceHandler(handler,
		connect.WithInterceptors(controller2.RequireOperatorInterceptor(app)))
	mux := http.NewServeMux()
	mux.Handle(path, connectHandler)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := apiv1connect.NewTelemetryServiceClient(server.Client(), server.URL)

	req := connect.NewRequest(&apiv1.QueryTelemetryRequest{
		AgentId: "query-test-agent",
		From:    nil, // defaults to the last hour, which covers `now`
	})
	req.Header().Set("Authorization", "Bearer "+operatorToken)
	resp, err := client.QueryTelemetry(context.Background(), req)
	if err != nil {
		t.Fatalf("QueryTelemetry: %v", err)
	}
	if len(resp.Msg.Points) != 1 {
		t.Fatalf("got %d points, want 1", len(resp.Msg.Points))
	}
	p := resp.Msg.Points[0]
	if p.AgentId != "query-test-agent" {
		t.Errorf("agent_id = %q, want query-test-agent", p.AgentId)
	}
	if p.Valid == nil || !p.Valid.Value {
		t.Errorf("valid = %v, want true", p.Valid)
	}
	if p.Payload == nil || p.Payload.Fields["battery_percent"].GetNumberValue() != 77 {
		t.Errorf("payload = %v, want battery_percent=77", p.Payload)
	}
}

// TestTelemetryLiveSSEDeliversPublishedPayload is Step 13's "live" requirement:
// GET /telemetry/live?agent_id= streams whatever live.Broadcaster.Publish
// sends for that agent, over a real HTTP connection.
func TestTelemetryLiveSSEDeliversPublishedPayload(t *testing.T) {
	app := bootstrapTestApp(t)
	operatorToken := mustCreateUser(t, app, "op-user", "operator")

	broadcaster := live.New()
	liveController := controller2.NewTelemetryLiveController(app, broadcaster)
	mux := http.NewServeMux()
	liveController.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/telemetry/live?agent_id=live-test-agent", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+operatorToken)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("GET /telemetry/live: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Give the handler a moment to reach broadcaster.Subscribe before we
	// publish -- otherwise Publish could fire before the subscription exists
	// and this specific message would never be delivered.
	time.Sleep(50 * time.Millisecond)
	broadcaster.Publish("live-test-agent", []byte(`{"x":1}`))

	scanner := bufio.NewScanner(resp.Body)
	var gotData string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			gotData = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	if gotData != `{"x":1}` {
		t.Errorf("SSE data = %q, want {\"x\":1}", gotData)
	}
}

func TestTelemetryLiveSSERequiresAuth(t *testing.T) {
	app := bootstrapTestApp(t)
	broadcaster := live.New()
	liveController := controller2.NewTelemetryLiveController(app, broadcaster)
	mux := http.NewServeMux()
	liveController.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/telemetry/live?agent_id=live-test-agent")
	if err != nil {
		t.Fatalf("GET /telemetry/live: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 with no Authorization header", resp.StatusCode)
	}
}
