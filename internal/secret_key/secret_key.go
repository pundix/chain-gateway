package secretkey

import (
	"github.com/pocketbase/pocketbase/core"
	collection "github.com/pundix/chain-gateway/internal"
	"github.com/pundix/chain-gateway/internal/client"
)

func Me() collection.Collection {
	return &SecretKeyCol{}
}

type SecretKeyCol struct {
}

func (c *SecretKeyCol) Apply(app core.App, cli *client.ChainGatewayClient) {
	app.OnRecordAfterCreateSuccess("secret_key").BindFunc(func(e *core.RecordEvent) error {
		sk := &client.SecretKey{
			Group:        e.Record.GetString("group"),
			Service:      e.Record.GetString("service"),
			AccessKey:    e.Record.GetString("access_key"),
			SecretKey:    e.Record.GetString("secret_key"),
			AllowOrigins: e.Record.GetString("allow_origins"),
			AllowIps:     e.Record.GetString("allow_ips"),
			RouteRules:   e.Record.GetString("route_rules"),
		}
		if err := cli.PostSecretKey(sk); err != nil {
			e.App.Logger().Error("create secret key fail", "error", err.Error())
			return err
		}
		e.App.Logger().Info("create secret key success", "group", sk.Group, "service", sk.Service)
		return e.Next()
	})

	app.OnRecordAfterUpdateSuccess("secret_key").BindFunc(func(e *core.RecordEvent) error {
		sk := &client.SecretKey{
			Group:        e.Record.GetString("group"),
			Service:      e.Record.GetString("service"),
			AllowOrigins: e.Record.GetString("allow_origins"),
			AllowIps:     e.Record.GetString("allow_ips"),
			AccessKey:    e.Record.GetString("access_key"),
			RouteRules:   e.Record.GetString("route_rules"),
			SecretKey:    e.Record.GetString("secret_key"),
		}
		if err := cli.PostSecretKey(sk); err != nil {
			e.App.Logger().Error("update secret key fail", "error", err.Error())
			return err
		}
		e.App.Logger().Info("update secret key success", "group", sk.Group, "service", sk.Service)
		return e.Next()
	})
}
