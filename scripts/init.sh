#!/bin/bash

go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc generate

# create chain-gateway d1 database
npx wrangler d1 create chain-gateway
npx wrangler d1 execute chain-gateway --remote --file=./schema.sql

# create gateway-checker queue
npx wrangler queues create gateway-checker 
