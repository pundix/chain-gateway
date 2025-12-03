# Chain Gateway

## Quick Start

```bash
go build -o cg .
```

Start the dashboard:
```bash
cg serve --http=127.0.0.1:8090
```
Use the dashboard to add chain nodes and health-check rules.

Start the gRPC proxy:
```bash
cg proxy --api=http://localhost:8090 --duration=1m
```
The gRPC proxy is available at localhost:50051.
It refreshes the list of healthy nodes from the dashboard every minute.

Tron Testnet gRPC demo:
```bash
grpcurl --proto ./api/api.proto --plaintext -H 'chainId:3448148188' -H 'accessKey:$ACCESS_KEY' localhost:50051 protocol.Wallet/GetChainParameters
```
Initialize Cloudflare D1:
> Make sure sqlc is installed.
```bash
chmod +x ./cloudflare/init_db.sh
./cloudflare/init_db.sh
```

Copy the Cloudflare D1 database ID into the workers JSONC configuration.
Deploy the Cloudflare Workers:
```bash
chmod +x ./cloudflare/deploy_workers.sh
./cloudflare/deploy_workers.sh
```