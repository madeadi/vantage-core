package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// 1788900001_add_agent_events_collection creates agent_events: the sink for
// throttled telemetry-validation events (and, later, other agent-lifecycle
// events such as presence changes) raised by cmd/core's telemetry pipeline.
//
// "type" and "kind" are free-text rather than a SelectField enum on purpose —
// the set of violation kinds is defined and validated in Go
// (see specs/mqtt_telemetry.specs.md Step 8's Violation.Kind), and pinning it
// to a DB-level enum here would mean a new migration every time that set
// changes. "signature" is the throttle key from Step 9: same
// (agent_id, signature) within a window collapses to one summary row instead
// of flooding the table.
func init() {
	m.Register(func(app core.App) error {
		if _, err := app.FindCollectionByNameOrId(collAgentEvents); err == nil {
			return nil // already present — safe to re-run
		}

		c := core.NewBaseCollection(collAgentEvents)
		c.Fields.Add(
			&core.TextField{Name: "agent_id", Required: true, Min: 1, Max: 255},
			&core.TextField{Name: "type", Required: true, Min: 1, Max: 100},
			&core.SelectField{Name: "severity", Required: true, MaxSelect: 1, Values: []string{"info", "warning", "error"}},
			&core.TextField{Name: "kind", Max: 100},
			&core.TextField{Name: "signature", Max: 255},
			&core.TextField{Name: "detail", Max: 2000},
			&core.JSONField{Name: "sample", MaxSize: 16000},
			&core.NumberField{Name: "count", OnlyInt: true, Required: true},
			&core.DateField{Name: "first_seen", Required: true},
			&core.DateField{Name: "last_seen", Required: true},
		)
		c.AddIndex("idx_agent_events_agent_last_seen", false, "agent_id, last_seen", "")

		c.ListRule = types.Pointer(operatorReadRule)
		c.ViewRule = types.Pointer(operatorReadRule)
		c.CreateRule = types.Pointer(adminOnlyRule)
		c.UpdateRule = types.Pointer(adminOnlyRule)
		c.DeleteRule = types.Pointer(adminOnlyRule)

		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId(collAgentEvents)
		if err != nil {
			return nil
		}
		return app.Delete(c)
	})
}
