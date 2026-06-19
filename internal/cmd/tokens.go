package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func tokensCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "Manage personal access tokens",
	}

	cmd.AddCommand(
		tokensCreateCmd(deps),
		tokensListCmd(deps),
		tokensDeleteCmd(deps),
	)

	return cmd
}

func tokensCreateCmd(deps *Deps) *cobra.Command {
	var name string
	var scope string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new personal access token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			pat, err := deps.Client.CreatePAT(ctx, name, scope)
			if err != nil {
				return fmt.Errorf("create token: %w", err)
			}

			fmt.Println("Token successfully created!")
			fmt.Printf("ID:    %s\n", pat.ID)
			fmt.Printf("Name:  %s\n", pat.Name)
			fmt.Printf("Scope: %s\n", pat.Scope)
			fmt.Println("\nIMPORTANT: Copy this token now. It will not be shown again.")
			fmt.Printf("Token: %s\n", pat.Token)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Name of the token (required)")
	cmd.Flags().StringVar(&scope, "scope", "deploy", "Scope of the token ('deploy' or 'all')")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func tokensListCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your personal access tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			pats, err := deps.Client.ListPATs(ctx)
			if err != nil {
				return fmt.Errorf("list tokens: %w", err)
			}

			if len(pats) == 0 {
				fmt.Println("No personal access tokens found. Create one with: heroctl tokens create --name <name>")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tNAME\tSCOPE\tCREATED\tLAST USED")
			for _, pat := range pats {
				lastUsed := "never"
				if pat.LastUsedAt != "" {
					lastUsed = pat.LastUsedAt
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", pat.ID, pat.Name, pat.Scope, pat.CreatedAt, lastUsed)
			}
			return w.Flush()
		},
	}

	return cmd
}

func tokensDeleteCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete/revoke a personal access token by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id := args[0]

			if err := deps.Client.DeletePAT(ctx, id); err != nil {
				return fmt.Errorf("delete token: %w", err)
			}

			fmt.Printf("Token %s deleted/revoked.\n", id)
			return nil
		},
	}

	return cmd
}
