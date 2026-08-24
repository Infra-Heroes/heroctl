package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// formatCredits renders a credit balance for display.
//
// MilliCredits is the authoritative value: it is the integer thousandths the
// API bills in, so formatting from it avoids the rounding a float64 introduces
// (0.001 has no exact binary representation). Credits is used only as a
// fallback for responses that omit the integer field.
func formatCredits(c *client.Credits) string {
	if c.MilliCredits == 0 {
		return fmt.Sprintf("%.3f", c.Credits)
	}
	m, sign := c.MilliCredits, ""
	if m < 0 {
		m, sign = -m, "-"
	}
	return fmt.Sprintf("%s%d.%03d", sign, m/1000, m%1000)
}

func creditsCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "credits",
		Short: "Show the org's credit balance",
		Long: `Show the current credit balance for your org.

heroctl cannot purchase credits M-bM-^@M-^T this command is read-only.`,
		Args: cobra.NoArgs,
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

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Org:      %s (%s)\n", org.Name, org.ID)
			fmt.Fprintf(out, "Credits:  %s\n", formatCredits(credits))
			return nil
		},
	}
}
