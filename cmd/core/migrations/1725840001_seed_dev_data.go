package migrations

import (
	"fmt"
	"log/slog"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// 1725840001_seed_dev_data seeds the historical dev fixtures into any config
// collection that is still empty, and creates a default "admin" user when the
// users collection is empty so the admin UI is reachable out of the box.
//
// A collection that already has rows (e.g. a real deployment provisioned via the
// admin UI) is left untouched.
func init() {
	m.Register(func(app core.App) error {
		if err := seedIfEmpty(app, collAgents, seedAgents); err != nil {
			return err
		}
		if err := seedIfEmpty(app, collMissions, seedMissions); err != nil {
			return err
		}
		if err := seedIfEmpty(app, collLayouts, seedLayouts); err != nil {
			return err
		}
		if err := seedIfEmpty(app, collAgentLayouts, seedAgentLayouts); err != nil {
			return err
		}
		return seedDefaultAdmin(app)
	}, func(app core.App) error {
		// down: remove the seeded rows and the default admin user. Rows a user
		// added by hand are matched by their seed identifier only.
		if err := deleteByField(app, collAgents, "agent_id", seededAgentIDs); err != nil {
			return err
		}
		if err := deleteByField(app, collMissions, "mission_id", seededMissionIDs); err != nil {
			return err
		}
		if err := deleteByField(app, collLayouts, "layout_id", seededLayoutIDs); err != nil {
			return err
		}
		if err := deleteByField(app, collAgentLayouts, "agent_id", []string{"70000000-0000-0000-0000-000000000002"}); err != nil {
			return err
		}
		return deleteByField(app, collUsers, "username", []string{"admin"})
	})
}

var (
	seededAgentIDs   = []string{"smallbot", "70000000-0000-0000-0000-000000000001", "70000000-0000-0000-0000-000000000002"}
	seededMissionIDs = []string{"from_kitchen", "to_kitchen"}
	seededLayoutIDs  = []string{"layout1", "layout", "40000000-0000-0000-0000-000000000001", "40000000-0000-0000-0000-000000000002"}
)

func seedAgents(app core.App, c *core.Collection) error {
	rows := []struct{ agentID, name, key string }{
		{"smallbot", "Small Bot", "dev-key-abc123"},
		{"70000000-0000-0000-0000-000000000001", "SPS MR 1", "sps-bot-1"},
		{"70000000-0000-0000-0000-000000000002", "SPS MR 2", "sps-bot-2"},
	}
	for _, a := range rows {
		r := core.NewRecord(c)
		r.Set("agent_id", a.agentID)
		r.Set("name", a.name)
		r.Set("key", a.key)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}

func seedMissions(app core.App, c *core.Collection) error {
	rows := []struct{ missionID, name, key string }{
		{"from_kitchen", "SPS From Kitchen", "key_from_kitchen"},
		{"to_kitchen", "SPS To Kitchen", "key_to_kitchen"},
	}
	for _, mi := range rows {
		r := core.NewRecord(c)
		r.Set("mission_id", mi.missionID)
		r.Set("name", mi.name)
		r.Set("key", mi.key)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}

func seedLayouts(app core.App, c *core.Collection) error {
	rows := []struct{ layoutID, name, coord string }{
		{"layout1", "Layout 1", "pixel"},
		{"layout", "Layout 2", "latlon"},
		{"40000000-0000-0000-0000-000000000001", "SPS Kitchen", "pixel"},
		{"40000000-0000-0000-0000-000000000002", "SPS Institution 1", "pixel"},
	}
	for _, l := range rows {
		r := core.NewRecord(c)
		r.Set("layout_id", l.layoutID)
		r.Set("name", l.name)
		r.Set("coordinate_system", l.coord)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}

func seedAgentLayouts(app core.App, c *core.Collection) error {
	r := core.NewRecord(c)
	r.Set("agent_id", "70000000-0000-0000-0000-000000000002")
	r.Set("layout_id", "40000000-0000-0000-0000-000000000001")
	r.Set("north_offset", 0.0)
	r.Set("transformation_matrix", [][]float64{
		{50.0, 0.0, 885.0},
		{2.764757726692997e-15, 49.99999999999999, 436.5},
		{0.0, 0.0, 1.0},
	})
	return app.Save(r)
}

func seedDefaultAdmin(app core.App) error {
	n, err := app.CountRecords(collUsers)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	users, err := app.FindCollectionByNameOrId(collUsers)
	if err != nil {
		return err
	}

	r := core.NewRecord(users)
	r.Set("username", "admin")
	r.Set("email", "admin@vantageos.local")
	r.Set("role", "admin")
	r.Set("verified", true)
	r.SetPassword("changeme123")
	if err := app.Save(r); err != nil {
		return err
	}
	slog.Warn("seeded default admin user — change the password immediately",
		"username", "admin", "password", "changeme123")
	return nil
}

func seedIfEmpty(app core.App, name string, seed func(core.App, *core.Collection) error) error {
	c, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		return fmt.Errorf("find collection %q: %w", name, err)
	}
	existing, err := app.FindAllRecords(name)
	if err != nil {
		return fmt.Errorf("list %q: %w", name, err)
	}
	if len(existing) > 0 {
		return nil
	}
	if err := seed(app, c); err != nil {
		return fmt.Errorf("seed %q: %w", name, err)
	}
	slog.Info("seeded config collection with dev fixtures", "name", name)
	return nil
}

func deleteByField(app core.App, coll, field string, values []string) error {
	for _, v := range values {
		rec, err := app.FindFirstRecordByData(coll, field, v)
		if err != nil {
			continue // not found — nothing to undo
		}
		if err := app.Delete(rec); err != nil {
			return err
		}
	}
	return nil
}
