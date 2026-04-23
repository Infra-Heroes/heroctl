package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/build"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the heroctl version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("heroctl %s\n", build.Version)
			return nil
		},
	}
}
