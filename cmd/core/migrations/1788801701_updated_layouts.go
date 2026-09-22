package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_3294824926")
		if err != nil {
			return err
		}

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(4, []byte(`{
			"help": "",
			"hidden": false,
			"id": "file731270465",
			"maxSelect": 0,
			"maxSize": 10,
			"mimeTypes": [
				"image/jpeg",
				"image/vnd.mozilla.apng",
				"image/webp"
			],
			"name": "pixel_file",
			"presentable": false,
			"protected": false,
			"required": false,
			"system": false,
			"thumbs": [],
			"type": "file"
		}`)); err != nil {
			return err
		}

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(5, []byte(`{
			"help": "e.g. [   [1.130474, 103.596000], // Southwest coordinates   [1.478400, 104.094500]  // Northeast coordinates ];",
			"hidden": false,
			"id": "json1390131564",
			"maxSize": 0,
			"name": "gis_bound",
			"presentable": false,
			"required": false,
			"system": false,
			"type": "json"
		}`)); err != nil {
			return err
		}

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_3294824926")
		if err != nil {
			return err
		}

		// remove field
		collection.Fields.RemoveById("file731270465")

		// remove field
		collection.Fields.RemoveById("json1390131564")

		return app.Save(collection)
	})
}
