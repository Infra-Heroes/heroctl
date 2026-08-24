package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func billingCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "billing",
		Short: "Show or set the billing details used on invoices",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			profile, err := deps.Client.GetBillingProfile(cmd.Context())
			if err != nil {
				// No profile is the normal state before a first purchase, so
				// it gets an instruction rather than an error.
				if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "no billing profile") {
					fmt.Fprintln(out, "No billing details set. Credits cannot be purchased until they are.")
					fmt.Fprintln(out, "Set them with: heroctl billing set --help")
					return nil
				}
				return fmt.Errorf("get billing profile: %w", err)
			}

			fmt.Fprintf(out, "Type:     %s\n", profile.RecipientType)
			fmt.Fprintf(out, "Name:     %s\n", profile.Name)
			fmt.Fprintf(out, "Email:    %s\n", profile.Email)
			fmt.Fprintf(out, "Address:  %s, %s %s, %s\n",
				profile.Street, profile.PostalCode, profile.City, profile.Country)
			if profile.VATNumber != "" {
				fmt.Fprintf(out, "VAT no:   %s\n", profile.VATNumber)
			}
			if profile.VATReverseCharge {
				fmt.Fprintf(out, "VAT:      reverse charge (0%%)\n")
			} else if profile.VATRate != "" {
				fmt.Fprintf(out, "VAT:      %s%%\n", profile.VATRate)
			}
			return nil
		},
	}
	cmd.AddCommand(billingSetCmd(deps))
	return cmd
}

func billingSetCmd(deps *Deps) *cobra.Command {
	var p client.BillingProfile

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Create or replace the billing details",
		Long: "Create or replace the billing details used on invoices.\n\n" +
			"Every credit purchase is invoiced, and the VAT depends on the country\n" +
			"and on whether the buyer is a business. A business in another EU country\n" +
			"that supplies a VAT number is invoiced at 0% under reverse charge.",
		Example: "  heroctl billing set --type business --name 'Acme GmbH' \\\n" +
			"    --email billing@acme.example --street 'Teststr. 1' \\\n" +
			"    --postal-code 10115 --city Berlin --country DE --vat-number DE123456789",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			p.RecipientType = strings.ToLower(strings.TrimSpace(p.RecipientType))
			p.Country = strings.ToUpper(strings.TrimSpace(p.Country))
			p.VATNumber = strings.ToUpper(strings.TrimSpace(p.VATNumber))

			// Checked here so a typo costs a round trip rather than a
			// confusing upstream validation error.
			if p.RecipientType != "consumer" && p.RecipientType != "business" {
				return errors.New(`--type must be "consumer" or "business"`)
			}
			if len(p.Country) != 2 {
				return errors.New("--country must be a two-letter ISO 3166-1 code, e.g. DE")
			}
			for _, f := range []struct{ flag, value string }{
				{"--name", p.Name},
				{"--email", p.Email},
				{"--street", p.Street},
				{"--postal-code", p.PostalCode},
				{"--city", p.City},
			} {
				if strings.TrimSpace(f.value) == "" {
					return fmt.Errorf("%s is required", f.flag)
				}
			}

			saved, err := deps.Client.PutBillingProfile(cmd.Context(), p)
			if err != nil {
				return fmt.Errorf("save billing profile: %w", err)
			}

			fmt.Fprintln(out, "Billing details saved.")
			if saved.VATReverseCharge {
				fmt.Fprintln(out, "Invoices will be issued at 0% under reverse charge.")
			} else if saved.VATRate != "" {
				fmt.Fprintf(out, "Invoices will carry %s%% VAT.\n", saved.VATRate)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&p.RecipientType, "type", "consumer", `"consumer" or "business"`)
	f.StringVar(&p.Name, "name", "", "name or company")
	f.StringVar(&p.Email, "email", "", "address invoices are sent to")
	f.StringVar(&p.Street, "street", "", "street and number")
	f.StringVar(&p.PostalCode, "postal-code", "", "postal code")
	f.StringVar(&p.City, "city", "", "city")
	f.StringVar(&p.Country, "country", "", "ISO 3166-1 alpha-2 country code, e.g. DE")
	f.StringVar(&p.VATNumber, "vat-number", "", "EU VAT number (business only; enables reverse charge)")
	f.StringVar(&p.OrganizationNumber, "organization-number", "", "company register number")
	return cmd
}
