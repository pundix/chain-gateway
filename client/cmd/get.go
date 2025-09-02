package cmd

import (
	"net/http"
	"os"

	"github.com/pundix/chain-gateway/pkg/client"
	"github.com/spf13/cobra"
)

func Get() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "get resource",
	}

	cgc := &client.ChainGatewayClient{
		Cli:      &http.Client{},
		User:     os.Getenv("CGC_USER"),
		Password: os.Getenv("CGC_PASSWORD"),
	}

	upstreamCmd := &cobra.Command{
		Use:   "upstream",
		Short: "get upstream",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cgc.GetUpstream()
		},
	}
	upstreamCmd.PersistentFlags().BoolVarP(&cgc.Ready, "ready", "r", false, "ready")
	cmd.PersistentFlags().StringVarP(&cgc.Source, "source", "s", "chainlist", "source")
	cmd.PersistentFlags().StringVarP(&cgc.ChainId, "chainId", "c", "", "chainId")

	ruleCmd := &cobra.Command{
		Use:   "rule",
		Short: "get rule",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cgc.GetRule()
		},
	}

	cmd.AddCommand(upstreamCmd)
	cmd.AddCommand(ruleCmd)
	return cmd
}
