# Chain Gateway

## Quick Start

```bash
go build -o cg .
```

Launch the dashboard.
```bash
cg serve --http=127.0.0.1:8090
```
You can use dashboard to add some chain nodes and health check rules.

Launch the grpc proxy.
```bash
cg proxy --api=http://localhost:8090 --duration=1m
```
Grpc proxy is available at `localhost:50051`.
Grpc proxy will refresh the health node from the dashboard every 1 minute.

Tron Testnet grpc demo.
```bash
grpcurl --proto ./api/api.proto --plaintext -H 'chainId:3448148188' -H 'accessKey:$ACCESS_KEY' localhost:50051 protocol.Wallet/GetChainParameters
```