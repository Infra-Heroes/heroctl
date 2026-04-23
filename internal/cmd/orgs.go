package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func orgsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orgs",
		Short: "Show org info and credit balance",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			org, err := deps.Client.GetOrg(ctx)
			if err != nil {
				return fmt.Errorf("get org: %w", err)
			}

			credits, err := deps.Client.GetCredits(ctx, org.ID)
			if err != nil {
				return fmt.Errorf("get credits: %w", err)
			}

			fmt.Printf("Name:     %s\n", org.Name)
			fmt.Printf("VM cap:   %d\n", org.VmCap)
			fmt.Printf("Credits:  %.3f\n", credits.Credits)
			return nil
		},
	}

	return cmd
}
