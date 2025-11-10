package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection("secret_key")
		collection.Fields.Add(&core.TextField{
			Name:     "group",
			Required: true,
		})
		collection.Fields.Add(&core.TextField{
			Name:     "service",
			Required: true,
		})
		collection.Fields.Add(&core.TextField{
			Name:                "access_key",
			AutogeneratePattern: "[a-z0-9]{32}",
			Required:            true,
		})
		collection.Fields.Add(&core.TextField{
			Name:                "secret_key",
			AutogeneratePattern: "[a-z0-9]{64}",
			Required:            true,
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
		collection.Fields.Add(&core.TextField{
			Name: "allow_origins",
		})
		collection.Fields.Add(&core.TextField{
			Name: "allow_ips",
		})
		collection.AddIndex("idx_secret_key_access_key", true, "access_key", "")
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("secret_key")
		if err != nil {
			return err
		}
		return app.Delete(collection)
	})
}
