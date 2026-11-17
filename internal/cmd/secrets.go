package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
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
		Short: "Set a secret (asked for hidden on a terminal, or read from a pipe)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			key := args[0]

			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}

			value, err := readSecretValue(os.Stdin, cmd.InOrStdin(), key)
			if err != nil {
				return err
			}

			if err := deps.Client.SetSecret(ctx, project.ID, key, value); err != nil {
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

// readSecretValue reads a secret's value. On a terminal it asks, with the input hidden.
// From a pipe or a file (a script, CI) it reads what is sent, so a secret can be set
// without a person at the keyboard: `printf %s "$VALUE" | heroctl secrets set KEY`.
// One trailing newline is dropped, because that is what echo and a text file add and
// not what anybody means to store.
func readSecretValue(stdin *os.File, in io.Reader, key string) (string, error) {
	var value string
	if term.IsTerminal(int(stdin.Fd())) { // #nosec G115 -- a file descriptor always fits in an int
		_, _ = fmt.Fprintf(os.Stderr, "Enter value for %q (input hidden): ", key)
		b, err := term.ReadPassword(int(stdin.Fd())) // #nosec G115 -- as above
		_, _ = fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read secret value: %w", err)
		}
		value = string(b)
	} else {
		b, err := io.ReadAll(io.LimitReader(in, 1<<20))
		if err != nil {
			return "", fmt.Errorf("read secret value: %w", err)
		}
		value = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	}
	if value == "" {
		return "", fmt.Errorf("secret value must not be empty")
	}
	return value, nil
}
