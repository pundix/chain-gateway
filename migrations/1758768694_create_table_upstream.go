package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection("upstream")
		collection.Fields.Add(&core.TextField{
			Name:     "chain_id",
			Required: true,
		})
		collection.Fields.Add(&core.SelectField{
			Name:     "source",
			Required: true,
			Values:   []string{"manual", "auto", "chainlist", "cosmos/chain-registry", "custom/eth_getLogs", "custom/eth_gasPrice", "custom/tron", "paid", "paid2", "custom/grpc"},
		})
		collection.Fields.Add(&core.JSONField{
			Name: "rpc",
		})
		collection.Fields.Add(&core.SelectField{
			Name:     "protocol",
			Required: true,
			Values:   []string{"jsonrpc", "grpc"},
		})
		collection.Fields.Add(&core.BoolField{
			Name: "ready",
		})
		collection.Fields.Add(&core.AutodateField{
			Name:     "created",
			OnCreate: true,
		})
		collection.Fields.Add(&core.AutodateField{
			Name:     "updated",
			OnCreate: true,
			OnUpdate: true,
		})
		collection.AddIndex("idx_upstream_chain_id", false, "chain_id", "")
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("upstream")
		if err != nil {
			return err
		}
		return app.Delete(collection)
	})
}
