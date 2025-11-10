package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	collection "github.com/pundix/chain-gateway/internal"
	"github.com/pundix/chain-gateway/internal/client"
	"github.com/pundix/chain-gateway/internal/config"
	secretkey "github.com/pundix/chain-gateway/internal/secret_key"
	"github.com/pundix/chain-gateway/internal/upstream"

	_ "github.com/pundix/chain-gateway/migrations"
)

func main() {
	app := pocketbase.New()

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Automigrate: true,
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		rootPath := os.Getenv("GATEWAY_API_URL")
		user := os.Getenv("GATEWAY_USER")
		password := os.Getenv("GATEWAY_PASSWORD")

		chainGatewayCli, err := client.NewChainGatewayClient(
			user,
			password,
			rootPath,
			&http.Client{
				Timeout: time.Second * 10,
			},
		)
		if err != nil {
			return err
		}
		collections := []collection.Collection{
			secretkey.Me(),
			upstream.Me(),
			config.Me(),
		}
		for _, col := range collections {
			col.Apply(e.App, chainGatewayCli)
		}
		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
