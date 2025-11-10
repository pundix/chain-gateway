package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		jsonData := `{
			"createRule": null,
			"deleteRule": null,
			"fields": [
				{
					"autogeneratePattern": "",
					"hidden": false,
					"id": "text3208210256",
					"max": 0,
					"min": 0,
					"name": "id",
					"pattern": "^[a-z0-9]+$",
					"presentable": false,
					"primaryKey": true,
					"required": true,
					"system": true,
					"type": "text"
				},
				{
					"hidden": false,
					"id": "_clone_nwbS",
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
				},
				{
					"autogeneratePattern": "",
					"hidden": false,
					"id": "_clone_qkn8",
					"max": 0,
					"min": 0,
					"name": "chain_id",
					"pattern": "",
					"presentable": false,
					"primaryKey": false,
					"required": true,
					"system": false,
					"type": "text"
				},
				{
					"hidden": false,
					"id": "_clone_28Rm",
					"maxSize": 0,
					"name": "rpc",
					"presentable": false,
					"required": false,
					"system": false,
					"type": "json"
				}
			],
			"id": "pbc_3380222617",
			"indexes": [],
			"listRule": null,
			"name": "ready_upstream",
			"system": false,
			"type": "view",
			"updateRule": null,
			"viewQuery": "select id, source, chain_id, rpc from upstream where ready = true",
			"viewRule": null
		}`

		collection := &core.Collection{}
		if err := json.Unmarshal([]byte(jsonData), &collection); err != nil {
			return err
		}

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_3380222617")
		if err != nil {
			return err
		}

		return app.Delete(collection)
	})
}
