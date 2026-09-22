package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_4178717264")
		if err != nil {
			return err
		}

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(3, []byte(`{
			"help": "",
			"hidden": false,
			"id": "json4209398483",
			"maxSize": 0,
			"name": "telemetry_schema",
			"presentable": false,
			"required": false,
			"system": false,
			"type": "json"
		}`)); err != nil {
			return err
		}

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_4178717264")
		if err != nil {
			return err
		}

		// remove field
		collection.Fields.RemoveById("json4209398483")

		return app.Save(collection)
	})
}
