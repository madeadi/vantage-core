package migrations

import (
	"fmt"
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// 1725840000_init_schema establishes the full cmd/core schema:
//   - users: "username" identity field + "role" single-select
//   - agent_groups / agents / missions / layouts / agent_layouts config
//     collections, each locked to role "admin" for every CRUD op
//   - agents.agent_group relation -> agent_groups
//
// Each block is guarded so this is safe to apply on a database that was already
// provisioned by the old imperative bootstrap code.
func init() {
	m.Register(func(app core.App) error {
		if err := initUsers(app); err != nil {
			return err
		}
		if err := initConfigCollections(app); err != nil {
			return err
		}
		return initAgentGroupRelation(app)
	}, func(app core.App) error {
		// down: drop the config collections (agents before agent_groups so the
		// relation goes first), then strip the added users fields.
		for _, name := range []string{collAgentLayouts, collAgents, collMissions, collLayouts, collAgentGroups} {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue
			}
			if err := app.Delete(c); err != nil {
				return err
			}
		}

		users, err := app.FindCollectionByNameOrId(collUsers)
		if err != nil {
			return err
		}
		users.Fields.RemoveByName("username")
		users.Fields.RemoveByName("role")
		users.PasswordAuth.IdentityFields = slices.DeleteFunc(
			users.PasswordAuth.IdentityFields,
			func(f string) bool { return f == "username" },
		)
		return app.Save(users)
	})
}

func initUsers(app core.App) error {
	users, err := app.FindCollectionByNameOrId(collUsers)
	if err != nil {
		return err
	}

	if users.Fields.GetByName("username") == nil {
		users.Fields.Add(&core.TextField{
			Name:                "username",
			Min:                 3,
			Max:                 150,
			Pattern:             `^[\w][\w\.\-]*$`,
			AutogeneratePattern: "users[0-9]{6}",
			Required:            true,
		})
		users.AddIndex("idx_users_username", true, "username COLLATE NOCASE", "")
	}

	if !slices.Contains(users.PasswordAuth.IdentityFields, "username") {
		users.PasswordAuth.IdentityFields = append(users.PasswordAuth.IdentityFields, "username")
	}

	if users.Fields.GetByName("role") == nil {
		users.Fields.Add(&core.SelectField{
			Name:      "role",
			Values:    []string{"admin", "operator", "viewer"},
			MaxSelect: 1,
			Required:  true,
		})
	}

	return app.Save(users)
}

func initConfigCollections(app core.App) error {
	specs := []struct {
		name   string
		fields []core.Field
		index  string // "" to skip; otherwise the unique column expr
	}{
		{
			// created before collAgents so the agent_group relation has a target
			name: collAgentGroups,
			fields: []core.Field{
				&core.TextField{Name: "name", Required: true, Min: 1, Max: 255},
				&core.TextField{Name: "description", Max: 1000},
			},
			index: "name",
		},
		{
			name: collAgents,
			fields: []core.Field{
				&core.TextField{Name: "agent_id", Required: true, Min: 1, Max: 255},
				&core.TextField{Name: "name", Max: 255},
				&core.TextField{Name: "key", Required: true, Min: 1, Max: 255},
			},
			index: "agent_id",
		},
		{
			name: collMissions,
			fields: []core.Field{
				&core.TextField{Name: "mission_id", Required: true, Min: 1, Max: 255},
				&core.TextField{Name: "name", Max: 255},
				&core.TextField{Name: "key", Required: true, Min: 1, Max: 255},
			},
			index: "mission_id",
		},
		{
			name: collLayouts,
			fields: []core.Field{
				&core.TextField{Name: "layout_id", Required: true, Min: 1, Max: 255},
				&core.TextField{Name: "name", Max: 255},
				&core.SelectField{Name: "coordinate_system", Values: []string{"pixel", "latlon"}, MaxSelect: 1},
			},
			index: "layout_id",
		},
		{
			name: collAgentLayouts,
			fields: []core.Field{
				&core.TextField{Name: "agent_id", Required: true, Min: 1, Max: 255},
				&core.TextField{Name: "layout_id", Required: true, Min: 1, Max: 255},
				&core.NumberField{Name: "north_offset"},
				&core.JSONField{Name: "transformation_matrix", MaxSize: 64000},
			},
			index: "", // an agent can map to several layouts and vice versa
		},
	}

	for _, spec := range specs {
		if _, err := app.FindCollectionByNameOrId(spec.name); err == nil {
			continue // already exists
		}

		c := core.NewBaseCollection(spec.name)
		for _, f := range spec.fields {
			c.Fields.Add(f)
		}
		if spec.index != "" {
			c.AddIndex(fmt.Sprintf("idx_%s_%s", spec.name, spec.index), true, spec.index, "")
		}
		c.ListRule = types.Pointer(adminOnlyRule)
		c.ViewRule = types.Pointer(adminOnlyRule)
		c.CreateRule = types.Pointer(adminOnlyRule)
		c.UpdateRule = types.Pointer(adminOnlyRule)
		c.DeleteRule = types.Pointer(adminOnlyRule)

		if err := app.Save(c); err != nil {
			return fmt.Errorf("create collection %q: %w", spec.name, err)
		}
	}
	return nil
}

func initAgentGroupRelation(app core.App) error {
	agents, err := app.FindCollectionByNameOrId(collAgents)
	if err != nil {
		return err
	}
	if agents.Fields.GetByName("agent_group") != nil {
		return nil
	}

	groups, err := app.FindCollectionByNameOrId(collAgentGroups)
	if err != nil {
		return err
	}
	agents.Fields.Add(&core.RelationField{
		Name:          "agent_group",
		CollectionId:  groups.Id,
		MaxSelect:     1,
		Required:      false,
		CascadeDelete: false, // deleting a group leaves its agents ungrouped
	})
	return app.Save(agents)
}
