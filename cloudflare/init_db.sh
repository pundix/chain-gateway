#!/bin/bash

sqlc generate --file ./cloudflare/sqlc.yaml
npx wrangler d1 create chain-gateway
npx wrangler d1 execute chain-gateway --remote --file=./cloudflare/schema.sql
npx wrangler d1 execute chain-gateway --remote --file=./cloudflare/auth.sql