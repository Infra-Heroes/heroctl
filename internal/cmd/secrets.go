package cmd

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func secretsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secrets",
	}

	set := &cobra.Command{
		Use:   "set <key>",
		Short: "Set or update a secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			fmt.Printf("Enter value for %s: ", key)
			valueBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("read value: %w", err)
			}
			value := strings.TrimSpace(string(valueBytes))
			if err := deps.Client.SetSecret(cmd.Context(), key, value); err != nil {
				return fmt.Errorf("set secret: %w", err)
			}
			fmt.Printf("Secret %q saved.\n", key)
			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List secret keys (values are never shown)",
		RunE: func(cmd *cobra.Command, args []string) error {
			secrets, err := deps.Client.ListSecrets(cmd.Context())
			if err != nil {
				return fmt.Errorf("list secrets: %w", err)
			}
			if len(secrets) == 0 {
				fmt.Println("No secrets. Add one with: heroctl secrets set <key>")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tKEY\tCREATED")
			for _, s := range secrets {
				fmt.Fprintf(w, "%d\t%s\t%s\n", s.ID, s.Key, s.CreatedAt)
			}
			return w.Flush()
		},
	}

	del := &cobra.Command{
		Use:   "delete <key>",
		Short: "Delete a secret by key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := deps.Client.DeleteSecret(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("delete secret: %w", err)
			}
			fmt.Printf("Secret %q deleted.\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(set, list, del)
	return cmd
}
