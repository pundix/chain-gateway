package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_3380222617")
		if err != nil {
			return err
		}

		// update collection data
		if err := json.Unmarshal([]byte(`{
			"viewQuery": "select id, name, source, chain_id, rpc from upstream where ready = true"
		}`), &collection); err != nil {
			return err
		}

		// remove field
		collection.Fields.RemoveById("_clone_nwbS")

		// remove field
		collection.Fields.RemoveById("_clone_qkn8")

		// remove field
		collection.Fields.RemoveById("_clone_28Rm")

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(1, []byte(`{
			"autogeneratePattern": "",
			"hidden": false,
			"id": "_clone_m4a7",
			"max": 0,
			"min": 0,
			"name": "name",
			"pattern": "",
			"presentable": false,
			"primaryKey": false,
			"required": false,
			"system": false,
			"type": "text"
		}`)); err != nil {
			return err
		}

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(2, []byte(`{
			"hidden": false,
			"id": "_clone_lcUa",
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

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(3, []byte(`{
			"autogeneratePattern": "",
			"hidden": false,
			"id": "_clone_OzfZ",
			"max": 0,
			"min": 0,
			"name": "chain_id",
			"pattern": "",
			"presentable": false,
			"primaryKey": false,
			"required": true,
			"system": false,
			"type": "text"
		}`)); err != nil {
			return err
		}

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(4, []byte(`{
			"hidden": false,
			"id": "_clone_0u7f",
			"maxSize": 0,
			"name": "rpc",
			"presentable": false,
			"required": false,
			"system": false,
			"type": "json"
		}`)); err != nil {
			return err
		}

		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("pbc_3380222617")
		if err != nil {
			return err
		}

		// update collection data
		if err := json.Unmarshal([]byte(`{
			"viewQuery": "select id, source, chain_id, rpc from upstream where ready = true"
		}`), &collection); err != nil {
			return err
		}

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(1, []byte(`{
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
		}`)); err != nil {
			return err
		}

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(2, []byte(`{
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
		}`)); err != nil {
			return err
		}

		// add field
		if err := collection.Fields.AddMarshaledJSONAt(3, []byte(`{
			"hidden": false,
			"id": "_clone_28Rm",
			"maxSize": 0,
			"name": "rpc",
			"presentable": false,
			"required": false,
			"system": false,
			"type": "json"
		}`)); err != nil {
			return err
		}

		// remove field
		collection.Fields.RemoveById("_clone_m4a7")

		// remove field
		collection.Fields.RemoveById("_clone_lcUa")

		// remove field
		collection.Fields.RemoveById("_clone_OzfZ")

		// remove field
		collection.Fields.RemoveById("_clone_0u7f")

		return app.Save(collection)
	})
}
