package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_1575545053")
		if err != nil {
			return err
		}

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(4, []byte(`{
			"hidden": false,
			"id": "select3368074316",
			"maxSelect": 1,
			"name": "protocol",
			"presentable": false,
			"required": false,
			"system": false,
			"type": "select",
			"values": [
				"jsonrpc",
				"grpc"
			]
		}`)); err != nil {
			return err
		}

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_1575545053")
		if err != nil {
			return err
		}

		// remove field
		collection.Fields.RemoveById("select3368074316")

		return app.Save(collection)
	})
}
