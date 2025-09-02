package main

import "github.com/pundix/chain-gateway/client/cmd"

func main() {
	if err := cmd.Commands().Execute(); err != nil {
		panic(err)
	}
}
