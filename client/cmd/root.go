package cmd

import (
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func Commands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cgc",
		Short: "chain gateway 2.0 cli",
	}
	cmd.AddCommand(Get())
	cmd.AddCommand(Import())
	cmd.AddCommand(Check())
	cmd.AddCommand(Gen())
	return cmd
}

func init() {
	godotenv.Load()
}
