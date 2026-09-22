package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// 1788900002_add_telemetry_settings_collection creates telemetry_settings: a
// per-group toggle for whether validated telemetry is persisted to the
// Postgres/TimescaleDB store (spec Step 12). Off by default — persist_enabled
// is left non-required so its zero value (false) is the default for a new
// row, and a group with no row at all is likewise treated as disabled.
//
// This is separate from agent_groups.telemetry_schema/telemetry_mapping
// because it is an operational toggle rather than part of the telemetry
// contract, and flips independently of it. The connection database (DSN,
// batch size, flush interval) stays in core.config.yaml as a bootstrap
// setting, per root CLAUDE.md.
func init() {
	m.Register(func(app core.App) error {
		if _, err := app.FindCollectionByNameOrId(collTelemetrySettings); err == nil {
			return nil // already present — safe to re-run
		}

		groups, err := app.FindCollectionByNameOrId(collAgentGroups)
		if err != nil {
			return err
		}

		c := core.NewBaseCollection(collTelemetrySettings)
		c.Fields.Add(
			&core.RelationField{
				Name:          "agent_group",
				CollectionId:  groups.Id,
				Required:      true,
				MaxSelect:     1,
				CascadeDelete: true, // settings are meaningless without their group
			},
			&core.BoolField{Name: "persist_enabled"},
		)
		c.AddIndex("idx_telemetry_settings_agent_group", true, "agent_group", "")

		c.ListRule = types.Pointer(adminOnlyRule)
		c.ViewRule = types.Pointer(adminOnlyRule)
		c.CreateRule = types.Pointer(adminOnlyRule)
		c.UpdateRule = types.Pointer(adminOnlyRule)
		c.DeleteRule = types.Pointer(adminOnlyRule)

		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId(collTelemetrySettings)
		if err != nil {
			return nil
		}
		return app.Delete(c)
	})
}
