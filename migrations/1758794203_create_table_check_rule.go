package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection("check_rule")
		collection.Fields.Add(&core.TextField{
			Name:     "chain_id",
			Required: true,
		})
		collection.Fields.Add(&core.TextField{
			Name:     "source",
			Required: true,
		})
		collection.Fields.Add(&core.JSONField{
			Name:     "rules",
			Required: true,
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
		collection.AddIndex("idx_check_rule_chain_id", false, "chain_id", "")
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("check_rule")
		if err != nil {
			return err
		}
		return app.Delete(collection)
	})
}
