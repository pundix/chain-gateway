package main

import (
	"database/sql"
	"log"
	"strings"

	"github.com/pundix/chain-gateway/pkg/checker"
	_ "github.com/syumai/workers/cloudflare/d1"
	"github.com/syumai/workers/cloudflare/queues"
)

func main() {
	queues.Consume(consumeBatch)
}

func consumeBatch(batch *queues.MessageBatch) error {
	var chainIds []string
	for _, msg := range batch.Messages {
		chainIdsStr := msg.Body.String()
		log.Printf("Received message: %s\n", chainIdsStr)
		chainIds = append(chainIds, strings.Split(chainIdsStr, ",")...)
	}
	batch.AckAll()
	check(chainIds)
	return nil
}

func check(chainIds []string) {
	db, err := sql.Open("d1", "DB")
	if err != nil {
		log.Printf("error opening DB: %s\n", err.Error())
		return
	}
	defer db.Close()
	checker.Check(db, chainIds)
}
