package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_1822414608")
		if err != nil {
			return err
		}

		// update field
		if err := collection.Fields.AddMarshaledJSONAt(2, []byte(`{
			"hidden": false,
			"id": "select1602912115",
			"maxSelect": 0,
			"name": "source",
			"presentable": false,
			"required": true,
			"system": false,
			"type": "select",
			"values": [
				"manual",
				"auto",
				"chainlist",
				"cosmos/chain-registry",
				"custom/eth_getLogs",
				"custom/eth_gasPrice",
				"custom/tron",
				"paid",
				"paid2",
				"custom/grpc",
				"custom/solana"
			]
		}`)); err != nil {
			return err
		}

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_1822414608")
		if err != nil {
			return err
		}

		// update field
		if err := collection.Fields.AddMarshaledJSONAt(2, []byte(`{
			"hidden": false,
			"id": "select1602912115",
			"maxSelect": 0,
			"name": "source",
			"presentable": false,
			"required": true,
			"system": false,
			"type": "select",
			"values": [
				"manual",
				"auto",
				"chainlist",
				"cosmos/chain-registry",
				"custom/eth_getLogs",
				"custom/eth_gasPrice",
				"custom/tron",
				"paid",
				"paid2",
				"custom/grpc"
			]
		}`)); err != nil {
			return err
		}

		return app.Save(collection)
	})
}
