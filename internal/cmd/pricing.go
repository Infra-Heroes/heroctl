package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// pricingCmd replaces the old "credits packages" command. There is no
// catalogue of bundles to list any more — a top-up is any amount between the
// bounds below — but the bounds and the rates still have to come from
// somewhere, and hardcoding either in the CLI is how a released binary ends up
// quoting a price the platform no longer charges.
//
// GET /api/v1/pricing needs no authentication, but this command does: the
// client sends a bearer token on every request, so an anonymous call would
// need a second request path through it. Not worth it for a command whose
// audience already has an account — the public rates are on the website.
func pricingCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "pricing",
		Short: "Show the platform rates and top-up limits",
		Long: `Show what the platform charges, in euros.

These are the rates the billing worker charges by, read from hero-api rather
than from anything baked into this binary. vCPU and RAM are counted per
replica; volume storage is billed while the volume exists, attached or not.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			p, err := deps.Client.GetPricing(cmd.Context())
			if err != nil {
				return fmt.Errorf("get pricing: %w", err)
			}

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "RESOURCE\tRATE (EUR)\tPER")
			fmt.Fprintf(w, "vCPU\t%s\thour, per replica\n", formatEuroMicro(p.VCPUHourMicroEUR))
			fmt.Fprintf(w, "RAM\t%s\tGB-hour, per replica\n", formatEuroMicro(p.GBRAMHourMicroEUR))
			fmt.Fprintf(w, "Volume storage\t%s\tGB-hour\n", formatEuroMicro(p.GBDiskHourMicroEUR))
			if err := w.Flush(); err != nil {
				return err
			}

			fmt.Fprintf(out, "\nTop-up:   %s to %s EUR per payment\n",
				formatEuro(p.MinTopUpMicroEUR), formatEuro(p.MaxTopUpMicroEUR))
			fmt.Fprintln(out, "          heroctl balance topup <amount>")
			return nil
		},
	}
}
