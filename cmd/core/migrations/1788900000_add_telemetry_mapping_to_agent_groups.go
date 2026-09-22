package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// 1788900000_add_telemetry_mapping_to_agent_groups adds the telemetry_mapping
// JSON field to agent_groups.
//
// ui/src/lib/agent-groups.ts and agent-group-sheet.tsx have read and written
// this field since the agent groups UI was added, but no migration ever
// created it — PocketBase silently drops unknown fields on save, so every
// mapping a developer configured through the UI was lost. This migration
// only adds the column; it cannot recover data that was never persisted.
func init() {
	m.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId(collAgentGroups)
		if err != nil {
			return err
		}
		if c.Fields.GetByName("telemetry_mapping") != nil {
			return nil // already present — safe to re-run
		}
		c.Fields.Add(&core.JSONField{Name: "telemetry_mapping", MaxSize: 64000})
		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId(collAgentGroups)
		if err != nil {
			return err
		}
		c.Fields.RemoveByName("telemetry_mapping")
		return app.Save(c)
	})
}
