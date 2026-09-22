package main

import (
	"encoding/json"
	"log/slog"

	"vantageos-core/cmd/core/config"
	grpc2 "vantageos-core/cmd/core/grpc"
	"vantageos-core/cmd/core/model"
	"vantageos-core/cmd/core/service"
	"vantageos-core/cmd/core/telemetry/mapping"
	"vantageos-core/cmd/core/telemetry/persistcfg"
	"vantageos-core/cmd/core/telemetry/registry"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Config collection names. Each row's logical identifier is stored in a
// dedicated text field (agent_id / mission_id / layout_id) rather than the
// PocketBase record id, so the existing slugs and UUIDs keep working unchanged.
//
// The collections themselves (and their API rules, indexes, and the users
// "username"/"role" fields) are created by the migrations in
// cmd/core/migrations, not here.
const (
	collAgents            = "agents"
	collMissions          = "missions"
	collAgentLayouts      = "agent_layouts"
	collAgentGroups       = "agent_groups"
	collTelemetrySettings = "telemetry_settings"
)

// --- loaders -----------------------------------------------------------------

func loadAllowedAgents(app core.App) ([]service.AllowedAgent, error) {
	records, err := app.FindAllRecords(collAgents)
	if err != nil {
		return nil, err
	}
	out := make([]service.AllowedAgent, 0, len(records))
	for _, r := range records {
		out = append(out, service.AllowedAgent{
			AgentID: model.AgentID(r.GetString("agent_id")),
			Name:    r.GetString("name"),
			Key:     r.GetString("key"),
		})
	}
	return out, nil
}

func loadMissions(app core.App) ([]config.MissionConfig, error) {
	records, err := app.FindAllRecords(collMissions)
	if err != nil {
		return nil, err
	}
	out := make([]config.MissionConfig, 0, len(records))
	for _, r := range records {
		out = append(out, config.MissionConfig{
			ID:   r.GetString("mission_id"),
			Name: r.GetString("name"),
			Key:  r.GetString("key"),
		})
	}
	return out, nil
}

func loadAgentLayouts(app core.App) ([]config.AgentLayoutConfig, error) {
	records, err := app.FindAllRecords(collAgentLayouts)
	if err != nil {
		return nil, err
	}
	out := make([]config.AgentLayoutConfig, 0, len(records))
	for _, r := range records {
		var matrix [][]float64
		if err := r.UnmarshalJSONField("transformation_matrix", &matrix); err != nil {
			slog.Error("agent_layouts: bad transformation_matrix, skipping row",
				"record", r.Id, "err", err)
			continue
		}
		out = append(out, config.AgentLayoutConfig{
			LayoutID:             r.GetString("layout_id"),
			AgentID:              model.AgentID(r.GetString("agent_id")),
			NorthOffset:          r.GetFloat("north_offset"),
			TransformationMatrix: matrix,
		})
	}
	return out, nil
}

// loadAgentGroupSchemas queries agent_groups and returns group record id ->
// telemetry_schema for every group that has one set. A group with no schema
// (JSONField default, read back as an empty/null types.JSONRaw) is omitted
// -- the schema registry treats a missing entry as no_contract.
func loadAgentGroupSchemas(app core.App) (map[string]json.RawMessage, error) {
	records, err := app.FindAllRecords(collAgentGroups)
	if err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(records))
	for _, r := range records {
		raw, ok := r.Get("telemetry_schema").(types.JSONRaw)
		if !ok || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		out[r.Id] = json.RawMessage(raw)
	}
	return out, nil
}

// loadAgentGroupMemberships queries agents and returns agent_id -> the
// record id of its agent_group relation. An agent with no group assigned is
// omitted -- the schema registry treats a missing entry as no_contract.
func loadAgentGroupMemberships(app core.App) (map[string]string, error) {
	records, err := app.FindAllRecords(collAgents)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(records))
	for _, r := range records {
		groupID := r.GetString("agent_group")
		if groupID == "" {
			continue
		}
		out[r.GetString("agent_id")] = groupID
	}
	return out, nil
}

// loadAgentGroupMappings queries agent_groups and returns group record id ->
// decoded telemetry_mapping for every group that has one set. A group with
// no mapping (JSONField default) is omitted -- mapping.Apply treats a
// missing entry (nil *mapping.Spec) as "use the documented defaults".
func loadAgentGroupMappings(app core.App) (map[string]*mapping.Spec, error) {
	records, err := app.FindAllRecords(collAgentGroups)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*mapping.Spec, len(records))
	for _, r := range records {
		raw, ok := r.Get("telemetry_mapping").(types.JSONRaw)
		if !ok || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var spec mapping.Spec
		if err := json.Unmarshal(raw, &spec); err != nil {
			slog.Error("agent_groups: bad telemetry_mapping, skipping row", "record", r.Id, "err", err)
			continue
		}
		out[r.Id] = &spec
	}
	return out, nil
}

// loadTelemetryPersistSettings queries telemetry_settings and returns
// agent_group record id -> persist_enabled. A group with no row is omitted
// -- persistcfg.Registry.PersistEnabled treats a missing entry as disabled,
// matching the telemetry_settings migration's off-by-default doc comment.
func loadTelemetryPersistSettings(app core.App) (map[string]bool, error) {
	records, err := app.FindAllRecords(collTelemetrySettings)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(records))
	for _, r := range records {
		groupID := r.GetString("agent_group")
		if groupID == "" {
			continue
		}
		out[groupID] = r.GetBool("persist_enabled")
	}
	return out, nil
}

// --- live reload -----------------------------------------------------------

// watchConfig binds PocketBase record hooks so that create/update/delete on a
// config collection re-queries that collection and swaps the fresh set into the
// relevant registry. No restart required.
//
// Caveat: reg-tokens and layout matrices update immediately, but an agent or
// mission with a live gRPC stream keeps it until it reconnects — token removal
// only blocks new registrations.
func watchConfig(app core.App, ar *service.AgentRegistry, mr *service.MissionRegistry, agentSrv grpc2.AgentServer, schemaRegistry *registry.Registry, persistRegistry *persistcfg.Registry) {
	reloadAgents := func() {
		agents, err := loadAllowedAgents(app)
		if err != nil {
			slog.Error("reload agents failed", "err", err)
			return
		}
		ar.SetAllowedAgents(agents)
		slog.Info("agents config reloaded", "count", len(agents))
	}
	reloadMissions := func() {
		missions, err := loadMissions(app)
		if err != nil {
			slog.Error("reload missions failed", "err", err)
			return
		}
		mr.SetAllowed(missions)
		slog.Info("missions config reloaded", "count", len(missions))
	}
	reloadAgentLayouts := func() {
		layouts, err := loadAgentLayouts(app)
		if err != nil {
			slog.Error("reload agent_layouts failed", "err", err)
			return
		}
		agentSrv.SetLayouts(layouts)
		slog.Info("agent_layouts config reloaded", "count", len(layouts))
	}
	// The telemetry schema registry depends on both collections: agents for
	// which group an agent belongs to, agent_groups for what that group's
	// contract is. Either changing (an admin editing a schema, or
	// reassigning an agent to a different group) must take effect
	// immediately -- see specs/mqtt_telemetry.specs.md Step 7.
	reloadAgentGroupMemberships := func() {
		memberships, err := loadAgentGroupMemberships(app)
		if err != nil {
			slog.Error("reload agent_group memberships failed", "err", err)
			return
		}
		schemaRegistry.SetAgentGroups(memberships)
		slog.Info("agent_group memberships reloaded", "count", len(memberships))
	}
	reloadAgentGroupSchemas := func() {
		schemas, err := loadAgentGroupSchemas(app)
		if err != nil {
			slog.Error("reload agent_group schemas failed", "err", err)
			return
		}
		if err := schemaRegistry.SetGroupSchemas(schemas); err != nil {
			// Not fatal: SetGroupSchemas still installs every schema that
			// did compile, so one group's mistake doesn't take every other
			// group's validation down with it.
			slog.Error("one or more agent_groups schemas failed to compile", "err", err)
		}
		slog.Info("agent_group schemas reloaded", "count", len(schemas))
	}
	// telemetry_mapping and persist_enabled both feed Step 12's persistence
	// gate (cmd/core/telemetry.go); nil persistRegistry means persistence is
	// off in this deployment (cfg.Telemetry.PersistenceEnabled false) -- skip
	// the reload entirely rather than tracking config no listener will ever
	// read.
	reloadAgentGroupMappings := func() {
		if persistRegistry == nil {
			return
		}
		mappings, err := loadAgentGroupMappings(app)
		if err != nil {
			slog.Error("reload agent_group telemetry_mapping failed", "err", err)
			return
		}
		persistRegistry.SetMappings(mappings)
		slog.Info("agent_group telemetry_mapping reloaded", "count", len(mappings))
	}
	reloadTelemetryPersistSettings := func() {
		if persistRegistry == nil {
			return
		}
		settings, err := loadTelemetryPersistSettings(app)
		if err != nil {
			slog.Error("reload telemetry_settings failed", "err", err)
			return
		}
		persistRegistry.SetPersistEnabled(settings)
		slog.Info("telemetry_settings reloaded", "count", len(settings))
	}

	bind := func(coll string, reload func()) {
		onChange := func(e *core.RecordEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			reload()
			return nil
		}
		app.OnRecordAfterCreateSuccess(coll).BindFunc(onChange)
		app.OnRecordAfterUpdateSuccess(coll).BindFunc(onChange)
		app.OnRecordAfterDeleteSuccess(coll).BindFunc(onChange)
	}

	bind(collAgents, reloadAgents)
	bind(collAgents, reloadAgentGroupMemberships)
	bind(collMissions, reloadMissions)
	bind(collAgentLayouts, reloadAgentLayouts)
	bind(collAgentGroups, reloadAgentGroupSchemas)
	bind(collAgentGroups, reloadAgentGroupMappings)
	bind(collTelemetrySettings, reloadTelemetryPersistSettings)
}
