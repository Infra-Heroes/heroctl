package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func secretsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secrets for a project",
	}

	cmd.AddCommand(
		secretsSetCmd(deps),
		secretsListCmd(deps),
		secretsDeleteCmd(deps),
	)

	return cmd
}

func secretsSetCmd(deps *Deps) *cobra.Command {
	var projectName string

	cmd := &cobra.Command{
		Use:   "set <key>",
		Short: "Set a secret (value is read securely from stdin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			key := args[0]

			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Enter value for %q (input hidden): ", key)
			valueBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return fmt.Errorf("read secret value: %w", err)
			}
			if len(valueBytes) == 0 {
				return fmt.Errorf("secret value must not be empty")
			}

			if err := deps.Client.SetSecret(ctx, project.ID, key, string(valueBytes)); err != nil {
				return fmt.Errorf("set secret: %w", err)
			}

			fmt.Printf("Secret %q set.\n", key)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}

func secretsListCmd(deps *Deps) *cobra.Command {
	var projectName string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List secret keys for a project (values are never shown)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}

			secrets, err := deps.Client.ListSecrets(ctx, project.ID)
			if err != nil {
				return fmt.Errorf("list secrets: %w", err)
			}

			if len(secrets) == 0 {
				fmt.Printf("No secrets. Set one with: heroctl secrets set <key> --project %s\n", projectName)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "KEY\tCREATED")
			for _, s := range secrets {
				_, _ = fmt.Fprintf(w, "%s\t%s\n", s.Key, s.CreatedAt)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}

func secretsDeleteCmd(deps *Deps) *cobra.Command {
	var projectName string

	cmd := &cobra.Command{
		Use:   "delete <key>",
		Short: "Delete a secret from a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			key := args[0]

			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}

			if err := deps.Client.DeleteSecret(ctx, project.ID, key); err != nil {
				return fmt.Errorf("delete secret: %w", err)
			}

			fmt.Printf("Secret %q deleted.\n", key)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}
