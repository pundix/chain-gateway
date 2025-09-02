package cmd

import (
	"net/http"
	"os"

	"github.com/pundix/chain-gateway/pkg/client"
	"github.com/spf13/cobra"
)

func Import() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "import resource",
	}

	cgc := &client.ChainGatewayClient{
		Cli:      &http.Client{},
		User:     os.Getenv("CGC_USER"),
		Password: os.Getenv("CGC_PASSWORD"),
	}

	upstreamCmd := &cobra.Command{
		Use:   "upstream",
		Short: "import upstream",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cgc.ImportUpstreams()
		},
	}

	ruleCmd := &cobra.Command{
		Use:   "rule",
		Short: "import rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cgc.ImportRules()
		},
	}

	cmd.PersistentFlags().StringVarP(&cgc.ImportFile, "file", "f", "", "import file")
	cmd.MarkFlagRequired("file")

	cmd.AddCommand(upstreamCmd)
	cmd.AddCommand(ruleCmd)
	return cmd
}
