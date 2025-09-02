package cmd

import (
	"net/http"
	"os"

	"github.com/pundix/chain-gateway/pkg/client"
	"github.com/spf13/cobra"
)

func Check() *cobra.Command {
	cgc := &client.ChainGatewayClient{
		Cli:      &http.Client{},
		User:     os.Getenv("CGC_USER"),
		Password: os.Getenv("CGC_PASSWORD"),
	}

	cmd := &cobra.Command{
		Use:   "check",
		Short: "check resource",
	}

	upstreamCmd := &cobra.Command{
		Use:   "upstream",
		Short: "check upstream",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cgc.CheckUpstream()
		},
	}
	upstreamCmd.PersistentFlags().StringVar(&cgc.ChainId, "chainIds", "", "chainIds")
	upstreamCmd.MarkFlagRequired("chainIds")

	secretCmd := &cobra.Command{
		Use:   "secret",
		Short: "check secret ak",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cgc.VrifySecret()
		},
	}
	secretCmd.PersistentFlags().StringVar(&cgc.AK, "ak", "", "ak")
	secretCmd.MarkFlagRequired("ak")

	cmd.AddCommand(upstreamCmd)
	cmd.AddCommand(secretCmd)
	return cmd
}
