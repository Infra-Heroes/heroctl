// Package cmd defines all heroctl Cobra commands.
package cmd

import (
	"fmt"
	"os"
	"time"

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
	var tokenFlag string

	root := &cobra.Command{
		Use:           "heroctl",
		Short:         "heroctl — CLI for the hero PaaS",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if !needsAuth(cmd) {
				return nil
			}

			var tok *auth.Token
			var err error
			if tokenFlag != "" {
				tok = &auth.Token{
					AccessToken: tokenFlag,
					TokenType:   "Bearer",
					Expiry:      time.Now().Add(365 * 24 * time.Hour),
				}
			} else {
				tok, err = auth.Load()
				if err != nil {
					return err
				}
			}
			deps.Token = tok
			serverURL := build.ServerURL
			if envURL := os.Getenv("HERO_API_URL"); envURL != "" {
				serverURL = envURL
			}
			deps.Client = client.New(serverURL, build.AuthDomain, build.ClientID, tok)
			return nil
		},
	}

	root.AddCommand(
		creditsCmd(&deps),
		billingCmd(&deps),
		loginCmd(),
		signupCmd(),
		orgsCmd(&deps),
		projectsCmd(&deps),
		deploymentsCmd(&deps),
		membersCmd(&deps),
		acceptInviteCmd(&deps),

		deployCmd(&deps),
		logsCmd(&deps),
		volumesCmd(&deps),
		secretsCmd(&deps),
		tokensCmd(&deps),
		creditsCmd(&deps),
		versionCmd(),
		validateCmd(),
	)

	root.PersistentFlags().StringVar(&tokenFlag, "token", "", "Authenticate using a personal access token")

	return root
}

// commandsWithoutAuth lists commands that must work without a stored token.
// Alongside the obvious ones, this covers Cobra's generated "help" and
// "completion" commands and the hidden completion commands a shell invokes on
// every TAB press — none of those talk to the API.
var commandsWithoutAuth = map[string]bool{
	"login":                         true,
	"signup":                        true,
	"validate":                      true,
	"version":                       true,
	"help":                          true,
	"completion":                    true,
	cobra.ShellCompRequestCmd:       true,
	cobra.ShellCompNoDescRequestCmd: true,
}

// needsAuth reports whether cmd requires an authenticated session.
//
// It walks the parent chain rather than testing cmd.Name() alone: Name()
// returns only the leaf, so "heroctl completion bash" reports "bash" and would
// otherwise be treated as an authenticated command.
func needsAuth(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if commandsWithoutAuth[c.Name()] {
			return false
		}
	}
	return true
}
