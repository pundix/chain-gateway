package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection("config")
		collection.Fields.Add(&core.TextField{
			Name:     "key",
			Required: true,
		})
		collection.Fields.Add(&core.JSONField{
			Name:     "value",
			Required: true,
		})
		collection.Fields.Add(&core.SelectField{
			Name:     "module",
			Required: true,
			Values:   []string{"upstream"},
		})
		collection.AddIndex("idx_config_module_key", true, "module,key", "")
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("config")
		if err != nil {
			return err
		}
		return app.Delete(collection)
	})
}
