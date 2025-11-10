package upstream

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	collection "github.com/pundix/chain-gateway/internal"
	"github.com/pundix/chain-gateway/internal/checker"
	"github.com/pundix/chain-gateway/internal/client"
	"github.com/pundix/chain-gateway/internal/config"
	"github.com/samber/lo"
)

func Me() collection.Collection {
	return &UpstreamCol{}
}

type UpstreamCol struct {
}

func (c *UpstreamCol) Apply(app core.App, cli *client.ChainGatewayClient) {
	app.OnRecordAfterCreateSuccess("upstream").BindFunc(func(e *core.RecordEvent) error {
		if !e.Record.GetBool("ready") || e.Record.GetString("protocol") != "jsonrpc" {
			return nil
		}
		var rpc []string
		if err := json.Unmarshal([]byte(e.Record.GetString("rpc")), &rpc); err != nil {
			return err
		}
		upstream := &client.Upstream{
			ChainId: e.Record.GetString("chain_id"),
			Source:  e.Record.GetString("source"),
			RPC:     strings.Join(rpc, ","),
		}
		cloudflareWorkerConfig, err := c.getCloudflareWorkerConfig(app)
		if err != nil {
			return err
		}
		if cloudflareWorkerConfig.Push {
			if err := cli.PostReadyUpstream(upstream); err != nil {
				e.App.Logger().Error("create upstream fail", "error", err.Error())
				return err
			}
		}
		e.App.Logger().Info("create upstream success", "source", upstream.Source, "chain_id", upstream.ChainId)
		return e.Next()
	})

	app.OnRecordAfterUpdateSuccess("upstream").BindFunc(func(e *core.RecordEvent) error {
		if !e.Record.GetBool("ready") || e.Record.GetString("protocol") != "jsonrpc" {
			return nil
		}
		var rpc []string
		if err := json.Unmarshal([]byte(e.Record.GetString("rpc")), &rpc); err != nil {
			return err
		}
		upstream := &client.Upstream{
			ChainId: e.Record.GetString("chain_id"),
			Source:  e.Record.GetString("source"),
			RPC:     strings.Join(rpc, ","),
		}
		cloudflareWorkerConfig, err := c.getCloudflareWorkerConfig(app)
		if err != nil {
			return err
		}
		if cloudflareWorkerConfig.Push {
			if err := cli.PostReadyUpstream(upstream); err != nil {
				e.App.Logger().Error("update upstream fail", "error", err.Error())
				return err
			}
		}
		e.App.Logger().Info("update upstream success", "source", upstream.Source, "chain_id", upstream.ChainId)
		return e.Next()
	})

	app.Cron().MustAdd("fetch-upstream", "*/5 * * * *", func() {
		upstreamFetchers := []UpstreamFetcher{}
		for _, fetcher := range upstreamFetchers {
			go fetcher.Fetch(cli.Cli, app)
		}
	})

	upstreamChecking := false
	app.Cron().MustAdd("check-upstream", "* * * * *", func() {
		if upstreamChecking {
			app.Logger().Debug("check upstream is running")
			return
		}
		upstreamChecking = true
		defer func() {
			upstreamChecking = false
		}()

		jsonrpcs, err := c.getRpcsGroupByChainId(app, client.PROTOCOL_JSONRPC)
		if err != nil {
			app.Logger().Error("get available rpc fail", "error", err.Error())
			return
		}
		grpcs, err := c.getRpcsGroupByChainId(app, client.PROTOCOL_GRPC)
		if err != nil {
			app.Logger().Error("get available grpc fail", "error", err.Error())
			return
		}

		checkRules, err := c.getCheckRulesGroupBySource(app)
		if err != nil {
			app.Logger().Error("get check rules fail", "error", err.Error())
			return
		}
		caches := checker.CheckCaches{}
		mainChecker := checker.New(cli.Cli, time.Minute)

		for source, rules := range checkRules {
			for _, rule := range rules {
				if rule.Disabled {
					continue
				}
				var urls []string
				var ok bool
				if rule.Protocol == client.PROTOCOL_GRPC {
					urls, ok = grpcs[rule.ChainId]
				} else {
					urls, ok = jsonrpcs[rule.ChainId]
				}
				if !ok {
					continue
				}
				// go func(checker checker.HealthChecker, urls []string, caches checker.CheckCaches) {
				ret, err := rule.Rules.Check(mainChecker, rule.ChainId, urls, caches)
				if err != nil {
					app.Logger().Error("check upstream fail", "source", source, "chainId", rule.ChainId, "error", err.Error())
					return
				}
				urls = lo.Filter(urls, func(u string, _ int) bool {
					return ret[u]
				})
				updateLen, err := c.saveReadyUpsteam(app, &client.Upstream{
					ChainId:  rule.ChainId,
					Source:   source,
					RPC:      strings.Join(urls, ","),
					Protocol: rule.Protocol,
				})
				if err != nil {
					app.Logger().Error("save ready upstream fail", "source", source, "chainId", rule.ChainId, "error", err.Error())
					return
				}
				if updateLen != 0 {
					app.Logger().Info("save ready upstream success", "source", source, "chainId", rule.ChainId, "count", updateLen)
				}
				// }(mainChecker, urls, caches)
			}
		}
	})
}

func (c *UpstreamCol) saveReadyUpsteam(app core.App, upstream *client.Upstream) (int, error) {
	updateLen := len(strings.Split(upstream.RPC, ","))
	record, err := app.FindFirstRecordByFilter(
		"upstream",
		"protocol = {:protocol} && ready = true && source = {:source} && chain_id = {:chain_id}",
		dbx.Params{"protocol": upstream.Protocol, "source": upstream.Source, "chain_id": upstream.ChainId},
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return -1, err
	}
	if record != nil && record.Get("rpc") == upstream.JsonStr() {
		return 0, nil
	}
	if record == nil {
		var collection *core.Collection
		collection, err = app.FindCollectionByNameOrId("upstream")
		if err != nil {
			return -1, err
		}
		record = core.NewRecord(collection)
		record.Set("chain_id", upstream.ChainId)
		record.Set("source", upstream.Source)
		record.Set("rpc", upstream.JsonStr())
		record.Set("protocol", upstream.Protocol)
		record.Set("ready", true)
	} else {
		var urls []string
		if err = record.UnmarshalJSONField("rpc", &urls); err != nil {
			return -1, err
		}
		updateLen = updateLen - len(urls)
		record.Set("rpc", upstream.JsonStr())
	}
	return updateLen, app.Save(record)
}

func (c *UpstreamCol) getRpcsGroupByChainId(app core.App, protocol client.Protocol) (map[string][]string, error) {
	records, err := app.FindAllRecords("upstream",
		dbx.HashExp{"protocol": protocol, "ready": false},
	)
	if err != nil {
		return nil, err
	}
	ret := map[string][]string{}
	for _, record := range records {
		chainId := record.GetString("chain_id")
		var urls []string
		if err = record.UnmarshalJSONField("rpc", &urls); err != nil {
			return nil, err
		}
		rpc, ok := ret[chainId]
		if !ok {
			ret[chainId] = urls
		} else {
			ret[chainId] = lo.Uniq(append(rpc, urls...))
		}
	}
	return ret, nil
}

func (c *UpstreamCol) getCheckRulesGroupBySource(app core.App) (map[string][]*client.CheckRule, error) {
	records, err := app.FindAllRecords("check_rule")
	if err != nil {
		return nil, err
	}
	checkRules := lo.Map(records, func(record *core.Record, index int) *client.CheckRule {
		var rules checker.HealthCheckConditionList
		if err = record.UnmarshalJSONField("rules", &rules); err != nil {
			return nil
		}
		return &client.CheckRule{
			ChainId:  record.GetString("chain_id"),
			Source:   record.GetString("source"),
			Protocol: client.Protocol(record.GetString("protocol")),
			Rules:    rules,
			Disabled: record.GetBool("disabled"),
		}
	})
	return lo.GroupBy(checkRules, func(item *client.CheckRule) string {
		return item.Source
	}), nil
}

func (c *UpstreamCol) getCloudflareWorkerConfig(app core.App) (*config.CloudflareWorkerConfig, error) {
	record, err := app.FindFirstRecordByFilter(
		"config",
		"module = 'upstream' && key = 'cloudflare_worker'",
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if record == nil {
		var collection *core.Collection
		collection, err = app.FindCollectionByNameOrId("config")
		if err != nil {
			return nil, err
		}
		record = core.NewRecord(collection)
		record.Set("key", "cloudflare_worker")
		record.Set("value", "{\"push\":false}")
		record.Set("module", "upstream")
		if err = app.Save(record); err != nil {
			return nil, err
		}
	}
	var config config.CloudflareWorkerConfig
	if err = record.UnmarshalJSONField("value", &config); err != nil {
		return nil, err
	}
	return &config, nil
}

type UpstreamFetcher interface {
	Fetch(*http.Client, core.App)
}
