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
	"github.com/Infra-Heroes/heroctl/internal/workspace"
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
		_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var deps Deps
	var tokenFlag string
	var workspaceFlag string

	root := &cobra.Command{
		Use:           "heroctl",
		Short:         "heroctl — CLI for the hero PaaS",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			ws, err := activateWorkspace(cmd, workspaceFlag)
			if err != nil {
				return err
			}
			if !needsAuth(cmd) {
				return nil
			}

			var tok *auth.Token
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
			if ws.Server != "" {
				serverURL = ws.Server
			}
			if envURL := os.Getenv("HERO_API_URL"); envURL != "" {
				serverURL = envURL
			}
			deps.Client = client.New(serverURL, build.AuthDomain, build.ClientID, tok)
			return nil
		},
	}

	root.AddCommand(
		balanceCmd(&deps),
		pricingCmd(&deps),
		billingCmd(&deps),
		loginCmd(),
		signupCmd(),
		orgsCmd(&deps),
		projectsCmd(&deps),
		deploymentsCmd(&deps),
		membersCmd(&deps),
		auditCmd(&deps),
		notificationsCmd(&deps),
		acceptInviteCmd(&deps),

		deployCmd(&deps),
		previewCmd(&deps),
		jobsCmd(&deps),
		ciCmd(&deps),
		logsCmd(&deps),
		volumesCmd(&deps),
		dbCmd(&deps),
		domainsCmd(&deps),
		secretsCmd(&deps),
		tokensCmd(&deps),
		versionCmd(),
		validateCmd(),
		initCmd(),
		workspaceCmd(),
	)

	root.PersistentFlags().StringVar(&tokenFlag, "token", "", "Authenticate using a personal access token")
	root.PersistentFlags().StringVar(&workspaceFlag, "workspace", "", "Workspace to use for this command (default: HERO_WORKSPACE, then the active one)")

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
	"init":                          true,
	"version":                       true,
	"help":                          true,
	"completion":                    true,
	"workspace":                     true,
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

// activateWorkspace picks the workspace for this command, points the token
// store at its login and fills --project from it when the command has that flag
// and it was not given.
func activateWorkspace(cmd *cobra.Command, flag string) (workspace.Workspace, error) {
	// The workspace commands manage the choice themselves; a stale one must not
	// stop somebody from fixing it.
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "workspace" {
			return workspace.Workspace{}, nil
		}
	}
	explicit := flag
	if explicit == "" {
		explicit = os.Getenv("HERO_WORKSPACE")
	}
	f, err := workspace.Load()
	if err != nil {
		return workspace.Workspace{}, err
	}
	name, ws, err := f.Active(explicit)
	if err != nil {
		return workspace.Workspace{}, err
	}
	auth.UseWorkspace(name)
	if ws.Project != "" {
		if pf := cmd.Flags().Lookup("project"); pf != nil && !pf.Changed {
			if err := pf.Value.Set(ws.Project); err != nil {
				return workspace.Workspace{}, err
			}
		}
	}
	return ws, nil
}
