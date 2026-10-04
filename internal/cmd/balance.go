package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// hero-api stores money as micro-euro and bills usage with integer arithmetic,
// so every amount that reaches heroctl is an exact integer. Formatting goes
// through these rather than through the float64 companion fields the API also
// sends: 0.01 has no exact binary representation, and a balance is not a place
// to inherit rounding error.
const (
	microEuroPerEuro = int64(1_000_000)
	microEuroPerCent = int64(10_000)
)

// formatEuro renders micro-euro as a two-decimal euro amount, rounding half
// away from zero — the same way hero-api renders money for invoices and mails,
// so the CLI and the invoice cannot disagree.
//
// For amounts a customer acts on: balances, budgets, top-up bounds.
func formatEuro(micro int64) string {
	neg := micro < 0
	if neg {
		micro = -micro
	}
	cents := (micro + microEuroPerCent/2) / microEuroPerCent
	out := fmt.Sprintf("%d.%02d", cents/100, cents%100)
	// A rounded-away negative must not print as "-0.00", which reads like a
	// bug rather than like zero.
	if neg && cents != 0 {
		return "-" + out
	}
	return out
}

// formatEuroMicro renders micro-euro at full precision.
//
// For amounts that are routinely a fraction of a cent: an hourly rate, or one
// usage charge in the ledger. Rounding those to cents would print a column of
// 0.00 and tell the reader nothing.
func formatEuroMicro(micro int64) string {
	neg := micro < 0
	if neg {
		micro = -micro
	}
	out := fmt.Sprintf("%d.%06d", micro/microEuroPerEuro, micro%microEuroPerEuro)
	if neg {
		return "-" + out
	}
	return out
}

// signedEuroMicro is formatEuroMicro with the sign always shown. In the ledger
// the sign is the most important column: a top-up and a usage charge must never
// be mistaken for one another at a glance.
func signedEuroMicro(micro int64) string {
	if micro >= 0 {
		return "+" + formatEuroMicro(micro)
	}
	return formatEuroMicro(micro)
}

// normalizeEuroAmount checks that s is an amount hero-api will accept and
// returns it with two decimals.
//
// Only the *shape* is checked here, never the bounds: what a top-up may be is
// hero-api's to decide and is published by `heroctl pricing`, and a limit
// hardcoded here would silently disagree with the server the day it changes.
// This exists so an obvious typo costs a round trip rather than an upstream
// validation error.
func normalizeEuroAmount(s string) (string, error) {
	s = strings.TrimSpace(s)
	malformed := fmt.Errorf("amount %q must be a positive euro amount with at most two decimals, e.g. 25.00", s)
	if s == "" {
		return "", malformed
	}
	whole, frac, hasPoint := strings.Cut(s, ".")
	if whole == "" || len(frac) > 2 || (hasPoint && frac == "") {
		return "", malformed
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return "", malformed
		}
	}
	for len(frac) < 2 {
		frac += "0"
	}
	return whole + "." + frac, nil
}

func balanceCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "Show the prepaid euro balance, history and top-up options",
		Long: `Show the current prepaid balance for your org, in euros.

Sub-commands cover the rest of billing: what the balance went on (ledger),
adding to it (topup) and what has been paid (payments). Rates and the top-up
limits are published by "heroctl pricing".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			ctx := cmd.Context()

			org, err := deps.Client.GetOrg(ctx)
			if err != nil {
				return fmt.Errorf("get org: %w", err)
			}
			balance, err := deps.Client.GetBalance(ctx, org.ID)
			if err != nil {
				return fmt.Errorf("get balance: %w", err)
			}

			_, _ = fmt.Fprintf(out, "Org:      %s (%s)\n", org.Name, org.ID)
			if balance.SpendableMicroEUR == nil {
				_, _ = fmt.Fprintf(out, "Balance:  %s EUR\n", formatEuro(balance.MicroEUR))
			} else {
				_, _ = fmt.Fprintf(out, "Billing:   %s\n", balance.BillingMode)
				_, _ = fmt.Fprintf(out, "Paid:      %s EUR\n", formatEuro(balance.MicroEUR))
				_, _ = fmt.Fprintf(out, "Promotion: %s EUR\n", formatEuro(balance.PromotionalMicroEUR))
				if balance.BillingMode == "internal" {
					_, _ = fmt.Fprintln(out, "Usage is metered; prepayment is not required.")
				} else {
					_, _ = fmt.Fprintf(out, "Spendable: %s EUR\n", formatEuro(*balance.SpendableMicroEUR))
				}
			}

			// Only set while the org is inside the window that follows a
			// balance hitting zero. Saying so is the whole point of the
			// window: deployments are still running, but not for long.
			if balance.GraceUntil != "" && balance.BillingMode != "internal" &&
				(balance.SpendableMicroEUR == nil || *balance.SpendableMicroEUR <= 0) {
				until := balance.GraceUntil
				if t, parseErr := time.Parse(time.RFC3339, balance.GraceUntil); parseErr == nil {
					until = t.Local().Format("2006-01-02 15:04")
				}
				_, _ = fmt.Fprintf(out, "\n⚠  Out of balance. Deployments stop after %s.\n", until)
				_, _ = fmt.Fprintf(out, "   Top up with: heroctl balance topup <amount>\n")
			}
			return nil
		},
	}

	cmd.AddCommand(balanceLedgerCmd(deps), balanceTopupCmd(deps), balancePaymentsCmd(deps))
	return cmd
}

func balanceLedgerCmd(deps *Deps) *cobra.Command {
	var limit, offset int
	cmd := &cobra.Command{
		Use:   "ledger",
		Short: "Show what the balance was spent on, newest first",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			ledger, err := deps.Client.GetLedger(cmd.Context(), limit, offset)
			if err != nil {
				return fmt.Errorf("get ledger: %w", err)
			}
			if len(ledger.Entries) == 0 {
				_, _ = fmt.Fprintln(out, "No ledger entries yet.")
				return nil
			}

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "DATE\tCHANGE (EUR)\tREASON")
			for _, e := range ledger.Entries {
				when := e.CreatedAt
				if t, parseErr := time.Parse(time.RFC3339, e.CreatedAt); parseErr == nil {
					when = t.Local().Format("2006-01-02 15:04")
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", when, signedEuroMicro(e.DeltaMicroEUR), e.Reason)
			}
			if err := w.Flush(); err != nil {
				return err
			}

			shown := int64(ledger.Offset) + int64(len(ledger.Entries))
			if shown < ledger.Total {
				_, _ = fmt.Fprintf(out, "\nShowing %d of %d. Next page: --offset %d\n", shown, ledger.Total, shown)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "entries per page")
	cmd.Flags().IntVar(&offset, "offset", 0, "entries to skip")
	return cmd
}

func balanceTopupCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "topup <amount>",
		Short: "Open a checkout to add euros to the balance",
		Long: `Open a Mollie checkout for a top-up, in euros.

The amount is free-form within the limits hero-api publishes; see
"heroctl pricing". The full gross amount paid lands on the balance.`,
		Example: "  heroctl balance topup 25\n  heroctl balance topup 7.50",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			amount, err := normalizeEuroAmount(args[0])
			if err != nil {
				return err
			}

			checkout, err := deps.Client.CreateCheckout(cmd.Context(), amount)
			if err != nil {
				// 428 means the org has no billing profile. Saying "checkout
				// failed" would leave the user guessing at something they can
				// fix in one command.
				if strings.Contains(err.Error(), "428") || strings.Contains(err.Error(), "billing profile") {
					return fmt.Errorf("billing details are required before topping up.\n" +
						"Set them with: heroctl billing set --help")
				}
				return fmt.Errorf("open checkout: %w", err)
			}

			_, _ = fmt.Fprintf(out, "Top-up:   %s EUR\n", checkout.AmountEUR)
			_, _ = fmt.Fprintf(out, "Payment:  %s\n\n", checkout.PaymentID)
			_, _ = fmt.Fprintf(out, "Complete the payment here:\n%s\n\n", checkout.CheckoutURL)
			// The balance is credited by Mollie's webhook, not on return from
			// the browser, so there is nothing for this process to wait on.
			_, _ = fmt.Fprintln(out, "The balance is credited once the payment confirms. Check with: heroctl balance")
			return nil
		},
	}
}

func balancePaymentsCmd(deps *Deps) *cobra.Command {
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
				_, _ = fmt.Fprintln(out, "No payments yet.")
				return nil
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "DATE\tSTATUS\tAMOUNT (EUR)\tINVOICE\tPAYMENT")
			for _, p := range payments {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					localDate(p.CreatedAt), p.Status, paymentAmount(p), invoiceOrDash(p), p.MollieID)
			}
			return w.Flush()
		},
	}
}

// paymentAmount is the gross amount as hero-api sent it — a decimal string
// copied from the row the invoice was issued from, not a number this process
// re-derived.
func paymentAmount(p client.Payment) string {
	if p.AmountEUR == "" {
		return "-"
	}
	return p.AmountEUR
}

// invoiceOrDash shows the invoice number for the purchase. Invoicing is
// best-effort inside hero-api's webhook — a balance is never held up by a
// billing hiccup — so a recent payment legitimately has none yet.
func invoiceOrDash(p client.Payment) string {
	if p.InvoiceNumber == "" {
		return "-"
	}
	return p.InvoiceNumber
}

// localDate renders an RFC3339 timestamp in the local zone, falling back to the
// raw value so an unparseable timestamp is shown rather than swallowed.
func localDate(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return ts
}
