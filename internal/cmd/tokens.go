package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
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
	var expires int
	var project string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new personal access token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if scope != "deploy" && scope != "read" && scope != "all" {
				return fmt.Errorf("--scope must be deploy, read or all")
			}
			if expires < 0 || expires > 3650 {
				return fmt.Errorf("--expires must be between 1 and 3650 days, or 0 for no end date")
			}
			opts := client.PATOptions{ExpiresInDays: expires}
			if project != "" {
				p, err := resolveProject(ctx, deps, project)
				if err != nil {
					return err
				}
				opts.ProjectID = p.ID
			}

			pat, err := deps.Client.CreatePAT(ctx, name, scope, opts)
			if err != nil {
				return fmt.Errorf("create token: %w", err)
			}

			fmt.Println("Token successfully created!")
			fmt.Printf("ID:      %s\n", pat.ID)
			fmt.Printf("Name:    %s\n", pat.Name)
			fmt.Printf("Scope:   %s\n", pat.Scope)
			if project != "" {
				fmt.Printf("Project: %s (this token reaches no other project)\n", project)
			}
			fmt.Printf("Expires: %s\n", expiryText(pat.ExpiresAt))
			fmt.Println("\nIMPORTANT: Copy this token now. It will not be shown again.")
			fmt.Printf("Token: %s\n", pat.Token)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Name of the token (required)")
	cmd.Flags().StringVar(&scope, "scope", "deploy", "What the token may do: deploy (a pipeline), read (look, never change) or all")
	cmd.Flags().IntVar(&expires, "expires", 0, "Days until the token stops working (0: no end date)")
	cmd.Flags().StringVar(&project, "project", "", "Hold the token to this project; it reaches no other")
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

			// Project names for the tokens held to one; best effort.
			names := map[string]string{}
			if projects, err := deps.Client.ListProjects(ctx); err == nil {
				for _, p := range projects {
					names[p.ID] = p.Name
				}
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tNAME\tSCOPE\tPROJECT\tEXPIRES\tLAST USED")
			for _, pat := range pats {
				lastUsed := "never"
				if pat.LastUsedAt != "" {
					lastUsed = pat.LastUsedAt
				}
				project := "all"
				if pat.ProjectID != nil {
					project = names[*pat.ProjectID]
					if project == "" {
						project = "(deleted)"
					}
				}
				expiry := expiryText(pat.ExpiresAt)
				if pat.Expired {
					expiry = "EXPIRED"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", pat.ID, pat.Name, pat.Scope, project, expiry, lastUsed)
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

// expiryText: the date a token stops working, or that it does not.
func expiryText(at *string) string {
	if at == nil || *at == "" {
		return "never"
	}
	if len(*at) >= 10 {
		return (*at)[:10]
	}
	return *at
}
