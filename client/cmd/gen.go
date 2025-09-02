package cmd

import (
	"net/http"
	"os"

	"github.com/pundix/chain-gateway/pkg/client"
	"github.com/spf13/cobra"
)

func Gen() *cobra.Command {
	cgc := &client.ChainGatewayClient{
		Cli:      &http.Client{},
		User:     os.Getenv("CGC_USER"),
		Password: os.Getenv("CGC_PASSWORD"),
	}

	cmd := &cobra.Command{
		Use:   "gen",
		Short: "generate resource",
	}

	secretCmd := &cobra.Command{
		Use:   "secret",
		Short: "generate secret ak sk pair",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cgc.GenSecret()
		},
	}
	secretCmd.PersistentFlags().StringVar(&cgc.Group, "group", "", "group")
	secretCmd.PersistentFlags().StringVar(&cgc.Service, "service", "", "service")
	secretCmd.MarkFlagRequired("group")
	secretCmd.MarkFlagRequired("service")

	cmd.AddCommand(secretCmd)
	return cmd
}
