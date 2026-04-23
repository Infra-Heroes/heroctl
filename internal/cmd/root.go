// Package cmd defines all heroctl Cobra commands.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/auth"
	"github.com/Infra-Heroes/heroctl/internal/build"
	"github.com/Infra-Heroes/heroctl/internal/client"
)

// Deps holds the shared dependencies injected into each command via closure.
// It is populated by the root PersistentPreRunE before any subcommand runs.
type Deps struct {
	Token  *auth.Token
	Client *client.Client
}

// Execute is the entry point called from main.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var deps Deps

	root := &cobra.Command{
		Use:           "heroctl",
		Short:         "heroctl — CLI for the hero PaaS",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// login and signup manage their own deps.
			if cmd.Name() == "login" || cmd.Name() == "signup" {
				return nil
			}

			tok, err := auth.Load()
			if err != nil {
				return err
			}
			deps.Token = tok
			deps.Client = client.New(build.ServerURL, build.AuthDomain, build.ClientID, tok)
			return nil
		},
	}

	root.AddCommand(
		loginCmd(),
		signupCmd(),
		orgsCmd(&deps),
		projectsCmd(&deps),
		deploymentsCmd(&deps),

		deployCmd(&deps),
		logsCmd(&deps),
		versionCmd(),
	)

	return root
}
