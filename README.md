# Chain Gateway

## Quick Start

```bash
go build -o cg .
```

Launch the dashboard.
```bash
cg serve --http=127.0.0.1:8090
```

Launch the grpc proxy.
```bash
cg proxy --api=http://localhost:8090 --duration=1m
```