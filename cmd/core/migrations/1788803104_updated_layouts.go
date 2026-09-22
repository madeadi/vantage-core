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

		// update field
		if err := collection.Fields.AddMarshaledJSONAt(4, []byte(`{
			"help": "",
			"hidden": false,
			"id": "file731270465",
			"maxSelect": 0,
			"maxSize": 10000,
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

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_3294824926")
		if err != nil {
			return err
		}

		// update field
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

		return app.Save(collection)
	})
}
