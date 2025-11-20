package cmd

import (
	"time"

	"github.com/pundix/chain-gateway/internal/proxy"
	"github.com/pundix/chain-gateway/pkg/pocketbase"
	"github.com/spf13/cobra"
)

func NewProxyCommand() *cobra.Command {
	p := &Proxier{}
	m := &cobra.Command{
		Use:   "proxy",
		Short: "proxy grpc endpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			return p.Proxy()
		},
	}
	m.Flags().DurationVar(&p.UpstreamCacheDuration, "duration", 5*time.Minute, "upstream cache duration")
	m.Flags().StringVar(&p.PocketbaseBaseApi, "api", "http://localhost:8090", "pocketbase api")
	return m
}

type Proxier struct {
	PocketbaseBaseApi     string
	UpstreamCacheDuration time.Duration
}

func (p *Proxier) Proxy() error {
	cli := pocketbase.New(p.PocketbaseBaseApi)
	grpc := proxy.NewGrpc(cli)
	grpc.Duration = p.UpstreamCacheDuration
	grpc.Fetch()
	return grpc.Proxy()
}
