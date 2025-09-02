package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	pkg_db "github.com/pundix/chain-gateway/pkg/db"
	"github.com/syumai/workers/cloudflare/cron"
	_ "github.com/syumai/workers/cloudflare/d1"
	"github.com/syumai/workers/cloudflare/queues"
)

const queueName = "QUEUE"

func task(ctx context.Context) error {
	db, err := sql.Open("d1", "DB")
	if err != nil {
		return err
	}
	defer db.Close()

	queries := pkg_db.New(db)
	kvCache, err := queries.GetKvCacheByKey(ctx, "all_chains")
	if err != nil {
		return err
	}

	chains := strings.Split(kvCache.Value, ",")

	q, err := queues.NewProducer(queueName)
	if err != nil {
		return err
	}

	for _, chain := range chains {
		fmt.Printf("Send chain: %s\n", chain)
		if err := q.SendText(chain); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	cron.ScheduleTask(task)
}
