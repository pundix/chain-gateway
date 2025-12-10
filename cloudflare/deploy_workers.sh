#!/bin/bash

cd ./cloudflare/workers/gateway-api
npx wrangler deploy

cd ../gateway-jsonrpc
npx wrangler deploy