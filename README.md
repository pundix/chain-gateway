# Chain-Gateway

## Pre-requisites

1. Have a Cloudflare account
2. Have already added a domain to Cloudflare

## Deploy

```shell
git clone git@github.com:pundix/chain-gateway.git
cd chain-gateway
cp .env.example .env
```

Fill in the .env file.

Run `export $(xargs <.env) && make init` to create the D1 database and related data tables.

Fill in the d1 database_id obtained from the previous step into the following configuration files:

* ./workers/proxy-worker/wrangler.tom 
* ./workers/check-worker/wrangler.tom
* ./workers/admin-worker/wrangler.tom
* ./workers/cron-worker/wrangler.tom

```toml
[[d1_databases]]
binding = "DB"
database_name = "chain-gateway"
database_id = "{ID}"
```

Fill in your added domain name in the following configuration:
* ./workers/proxy-worker/wrangler.tom

```toml
routes = [
  { pattern = "chain-gateway.{your-subdomain}", custom_domain = true }
]
```

Run `export $(xargs <.env) && make deploy` to deploy.

Prepare the Loki access address and credentials to fill in the following configuration(Optional):
* ./workers/log-worker/wrangler.jsonc

```jsonc
	/**
	 * Environment Variables
	 * https://developers.cloudflare.com/workers/wrangler/configuration/#environment-variables
	 */
	"vars": { "LOKI_CREDENTIALS": "{loki-credentials}", "LOKI_PUSH_URL": "https://{loki-endpoint}/loki/api/v1/push" }
```

Deploy the log-worker(Optional).
```shell
cd ./workers/log-worker
npx wrangler deploy
```

Add health check rules.

You can use the API provided by admin-worker to add rules.

## Get Started

Generate AKSK.
You can use the API provided by admin-worker to generate it.

Specify the network to access via the chainId query parameter.

```shell
curl -i --request POST \
     --url https://chain-gateway.{your-subdomain}/v2/{your-ak}\?chainId\=1 \
     --header 'content-type: application/json' \
     --data '
{
  "id": 1,
  "jsonrpc": "2.0",
  "method": "web3_clientVersion"
}
'
```

Specify the data source via the source query parameter.

>If the source parameter is not specified, multiple data sources will be merged by default.

```shell
curl -i --request POST \
     --url https://chain-gateway.{your-subdomain}/v2/{your-ak}?chainId=theta-testnet-001&source={your-source} \
     --header 'content-type: application/json' \
     --data '
{
  "id": 1,
  "jsonrpc": "2.0",
  "method": "status",
  "params": []
}
'
```

## Health Check Rules
Currently, two check strategies are supported:
* ValueMatch
* BlockHeight

### ValueMatch
The ValueMatch check strategy returns results by comparing expected and actual values.
```json
{
  "checkStrategy": "ValueMatch",
  "payload": "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"status\",\"params\":[]}",
  "matchers": [
    {
      // Match types supported: = | !=
      "matchType": "=",
      // Nested fields, traversed using the . symbol
      "key": "result.node_info.other.tx_index",
      "value": "on"
    },
    {
      "matchType": "=",
      "key": "result.sync_info.catching_up",
      "value": "false"
    }
  ]
}
```

### BlockHeight

The BlockHeight check strategy uses the max() function to get the maximum height of the current chain. It compares the latest heights (including historical heights) obtained from all nodes with the max. If any node's height does not satisfy max - height < value (<= value), it returns false.

```json
{
  "checkStrategy": "BlockHeight",
  "payload": "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"status\",\"params\":[]}",
  "matchers": [
    {
       // Match types supported: < | <=
      "matchType": "<",
      "key": "result.sync_info.latest_block_height",
      "value": "10"
    }
  ]
}  
```

### Submit Rules

When you need to update rules, you can add or modify JSON files in check_rule/*. Each JSON file corresponds to the check rules for one data source.

```shell
curl -X POST \
     -H "Content-Type: multipart/form-data" \
     --form "source={your source}" \
     --form "file=@/{your-project-path}/check_rule/{your rules}.json" \
     https://{your-admin-worker-access-domain}/v1/rule/import
```
## Generate AKSK

```shell
curl -i --request POST \
     --url https://{your-admin-worker-access-domain}/v1/secret/gen \
     --header 'content-type: application/json' \
     --data '
{
    "group":"{your group}",
    "service":"{your service}"
}
'
```



