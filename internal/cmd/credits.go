package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

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
	cmd := &cobra.Command{
		Use:   "credits",
		Short: "Show the credit balance, history and top-up options",
		Long: `Show the current credit balance for your org.

Sub-commands cover the rest of billing: what the credits went on (ledger),
what can be bought (packages), buying it (topup) and what was bought
(payments).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			ctx := cmd.Context()

			org, err := deps.Client.GetOrg(ctx)
			if err != nil {
				return fmt.Errorf("get org: %w", err)
			}
			credits, err := deps.Client.GetCredits(ctx, org.ID)
			if err != nil {
				return fmt.Errorf("get credits: %w", err)
			}

			fmt.Fprintf(out, "Org:      %s (%s)\n", org.Name, org.ID)
			fmt.Fprintf(out, "Credits:  %s\n", formatCredits(credits))

			// Only set while the org is inside the window that follows a
			// balance hitting zero. Saying so is the whole point of the
			// window: deployments are still running, but not for long.
			if credits.GraceUntil != "" {
				until := credits.GraceUntil
				if t, parseErr := time.Parse(time.RFC3339, credits.GraceUntil); parseErr == nil {
					until = t.Local().Format("2006-01-02 15:04")
				}
				fmt.Fprintf(out, "\n⚠  Out of credits. Deployments stop after %s.\n", until)
				fmt.Fprintf(out, "   Top up with: heroctl credits topup <package>\n")
			}
			return nil
		},
	}

	cmd.AddCommand(creditsLedgerCmd(deps), creditsPackagesCmd(deps), creditsTopupCmd(deps), creditsPaymentsCmd(deps))
	return cmd
}

func creditsLedgerCmd(deps *Deps) *cobra.Command {
	var limit, offset int
	cmd := &cobra.Command{
		Use:   "ledger",
		Short: "Show what credits were spent on, newest first",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			ledger, err := deps.Client.GetLedger(cmd.Context(), limit, offset)
			if err != nil {
				return fmt.Errorf("get ledger: %w", err)
			}
			if len(ledger.Entries) == 0 {
				fmt.Fprintln(out, "No ledger entries yet.")
				return nil
			}

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "DATE\tCHANGE\tREASON")
			for _, e := range ledger.Entries {
				when := e.CreatedAt
				if t, parseErr := time.Parse(time.RFC3339, e.CreatedAt); parseErr == nil {
					when = t.Local().Format("2006-01-02 15:04")
				}
				// The sign is the most important column: a top-up and a usage
				// charge must never be mistaken for one another at a glance.
				fmt.Fprintf(w, "%s\t%+.3f\t%s\n", when, e.DeltaCredits, e.Reason)
			}
			if err := w.Flush(); err != nil {
				return err
			}

			shown := int64(ledger.Offset) + int64(len(ledger.Entries))
			if shown < ledger.Total {
				fmt.Fprintf(out, "\nShowing %d of %d. Next page: --offset %d\n", shown, ledger.Total, shown)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "entries per page")
	cmd.Flags().IntVar(&offset, "offset", 0, "entries to skip")
	return cmd
}

func creditsPackagesCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "packages",
		Short: "List the purchasable credit packages",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			packages, err := deps.Client.ListCreditPackages(cmd.Context())
			if err != nil {
				return fmt.Errorf("list packages: %w", err)
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PACKAGE\tPRICE\tCREDITS\tBONUS")
			for _, p := range packages {
				bonus := "-"
				if p.BonusPercent > 0 {
					bonus = fmt.Sprintf("+%d%%", p.BonusPercent)
				}
				fmt.Fprintf(w, "%s\t%s EUR\t%d\t%s\n", p.ID, p.AmountEUR, p.Credits, bonus)
			}
			return w.Flush()
		},
	}
}

func creditsTopupCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "topup <package>",
		Short: "Open a checkout for a credit package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			checkout, err := deps.Client.CreateCheckout(cmd.Context(), args[0])
			if err != nil {
				// 428 means the org has no billing profile. Saying "checkout
				// failed" would leave the user guessing at something they can
				// fix in one command.
				if strings.Contains(err.Error(), "428") || strings.Contains(err.Error(), "billing profile") {
					return fmt.Errorf("billing details are required before buying credits.\n" +
						"Set them with: heroctl billing set --help")
				}
				return fmt.Errorf("open checkout: %w", err)
			}

			fmt.Fprintf(out, "Package:  %s (%s EUR)\n", checkout.Package, checkout.AmountEUR)
			fmt.Fprintf(out, "Payment:  %s\n\n", checkout.PaymentID)
			fmt.Fprintf(out, "Complete the payment here:\n%s\n\n", checkout.CheckoutURL)
			// Credits arrive via Mollie's webhook, not on return from the
			// browser, so there is nothing for this process to wait on.
			fmt.Fprintln(out, "Credits are booked once the payment confirms. Check with: heroctl credits")
			return nil
		},
	}
}

func creditsPaymentsCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "payments",
		Short: "Show the payment history",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			payments, err := deps.Client.ListPayments(cmd.Context())
			if err != nil {
				return fmt.Errorf("list payments: %w", err)
			}
			if len(payments) == 0 {
				fmt.Fprintln(out, "No payments yet.")
				return nil
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "DATE\tSTATUS\tCREDITS\tPAYMENT")
			for _, p := range payments {
				when := p.CreatedAt
				if t, parseErr := time.Parse(time.RFC3339, p.CreatedAt); parseErr == nil {
					when = t.Local().Format("2006-01-02 15:04")
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", when, p.Status, p.MilliCredits/1000, p.MollieID)
			}
			return w.Flush()
		},
	}
}
