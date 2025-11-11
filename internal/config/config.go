package config

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	collection "github.com/pundix/chain-gateway/internal"
	"github.com/pundix/chain-gateway/internal/client"
)

func Me() collection.Collection {
	return &ConfigCol{}
}

type ConfigCol struct {
}

func (c *ConfigCol) Apply(app core.App, cli *client.ChainGatewayClient) {
	app.OnCollectionAfterCreateSuccess("config").BindFunc(func(e *core.CollectionEvent) error {
		supportedChains := core.NewRecord(e.Collection)
		chains := []string{
			"1",
			"11155111",
			"8453",
			"84532",
			"56",
			"97",
			"42161",
			"421614",
			"43114",
			"43113",
			"137",
			// "80001"
			"80002",
			"10",
			"11155420",
		}
		bytes, _ := json.Marshal(chains)
		supportedChains.Set("key", "supported_chains")
		supportedChains.Set("value", string(bytes))
		supportedChains.Set("module", "upstream")
		if err := e.App.Save(supportedChains); err != nil {
			return err
		}

		cloudflareWorkerConfig := CloudflareWorkerConfig{
			Push: false,
		}
		bytes, _ = json.Marshal(cloudflareWorkerConfig)
		cloudflareWorker := core.NewRecord(e.Collection)
		cloudflareWorker.Set("key", "cloudflare_worker")
		cloudflareWorker.Set("value", string(bytes))
		cloudflareWorker.Set("module", "upstream")
		if err := e.App.Save(cloudflareWorker); err != nil {
			return err
		}

		healthCheckConfig := HealthCheckConfig{
			Grpc:    false,
			Jsonrpc: false,
		}
		bytes, _ = json.Marshal(healthCheckConfig)
		healthCheck := core.NewRecord(e.Collection)
		healthCheck.Set("key", "health_check")
		healthCheck.Set("value", string(bytes))
		healthCheck.Set("module", "upstream")
		if err := e.App.Save(healthCheck); err != nil {
			return err
		}
		return e.Next()
	})

	app.OnRecordAfterUpdateSuccess("config").BindFunc(func(e *core.RecordEvent) error {
		key := e.Record.Get("key")
		switch key {
		case "health_check":
			healthCheck := &HealthCheckConfig{}
			if err := e.Record.UnmarshalJSONField("value", healthCheck); err != nil {
				return err
			}
			if err := c.configHealthCheck(e.App, healthCheck); err != nil {
				e.App.Logger().Error("config health check fail", "error", err.Error())
				return err
			}
			e.App.Logger().Info("config health check success", "grpc", healthCheck.Grpc, "jsonrpc", healthCheck.Jsonrpc)
		case "route_rules":
			err := cli.PostConfig(&client.Config{
				Key:    "route_rules",
				Value:  e.Record.GetString("value"),
				Module: e.Record.GetString("module"),
			})
			if err != nil {
				return err
			}
			e.App.Logger().Info("update config route_rules success")
		}
		return e.Next()
	})

	app.OnRecordAfterCreateSuccess("config").BindFunc(func(e *core.RecordEvent) error {
		key := e.Record.Get("key")
		if key != "route_rules" {
			return nil
		}
		err := cli.PostConfig(&client.Config{
			Key:    "route_rules",
			Value:  e.Record.GetString("value"),
			Module: e.Record.GetString("module"),
		})
		if err != nil {
			return err
		}
		e.App.Logger().Info("create config route_rules success")
		return e.Next()
	})
}

type HealthCheckConfig struct {
	Grpc    bool `json:"grpc"`
	Jsonrpc bool `json:"jsonrpc"`
}

func (c *ConfigCol) configHealthCheck(app core.App, conf *HealthCheckConfig) error {
	records, err := app.FindAllRecords("check_rule")
	if err != nil {
		app.Logger().Error("find check_rule fail", "error", err.Error())
		return err
	}

	for _, record := range records {
		disabled := record.GetBool("disabled")
		if record.Get("protocol") == "" {
			record.Set("protocol", client.PROTOCOL_JSONRPC)
		}
		protocol := client.Protocol(record.GetString("protocol"))
		switch protocol {
		case client.PROTOCOL_JSONRPC:
			disabled = !conf.Jsonrpc
		case client.PROTOCOL_GRPC:
			disabled = !conf.Grpc
		}
		record.Set("disabled", disabled)
		if err := app.Save(record); err != nil {
			return err
		}
	}
	return nil
}

type CloudflareWorkerConfig struct {
	Push bool `json:"push"`
}
