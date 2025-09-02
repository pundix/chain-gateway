.PHONY: dev
dev:
	npx wrangler dev --remote

.PHONY: build
build:
	# go run github.com/syumai/workers/cmd/workers-assets-gen@v0.30.2 -mode=go
	# GOOS=js GOARCH=wasm go build -ldflags '-s -w' -o ./build/app.wasm .
	go run github.com/syumai/workers/cmd/workers-assets-gen
	tinygo build -o ./build/app.wasm -target wasm -no-debug ./...

.PHONY: deploy
deploy:
	# npx wrangler deploy --minify
	npx wrangler deploy