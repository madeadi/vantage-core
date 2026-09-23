package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// 1788900003_add_paths_to_agent_events adds the "paths" JSON field to
// agent_events: the JSON Pointer paths a violation's Detail already
// describes in prose (e.g. "missing required field(s): battery_percent"),
// kept structured so the UI's contract & health page (spec Step 14) can
// show the failing path(s) directly rather than parsing them back out of
// Detail's free text. validate.Violation has always computed this (see
// cmd/core/telemetry/validate/validate.go's classify); it just wasn't
// persisted -- see cmd/core/telemetry/events/pbsink.go's Emit, updated
// alongside this migration to start writing it.
func init() {
	m.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId(collAgentEvents)
		if err != nil {
			return err
		}
		if c.Fields.GetByName("paths") != nil {
			return nil // already present — safe to re-run
		}
		c.Fields.Add(&core.JSONField{Name: "paths", MaxSize: 4000})
		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId(collAgentEvents)
		if err != nil {
			return err
		}
		c.Fields.RemoveByName("paths")
		return app.Save(c)
	})
}
