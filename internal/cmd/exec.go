package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	"github.com/Infra-Heroes/heroctl/internal/client"
	"github.com/Infra-Heroes/heroctl/internal/exec"
)

// ExitCodeError makes heroctl exit with the exit code of the remote command, without an
// error message.
type ExitCodeError struct{ Code int }

func (e *ExitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// openTerminal is replaced in tests.
var openTerminal = exec.OpenStdTerminal

const execHelpNote = `Exec must be enabled per deployment first; an editor can do that. It needs an
interactive login: run "heroctl login". A personal access token (--token) is refused.`

// connectApp issues an exec session for the app and opens the SSH connection.
func connectApp(ctx context.Context, deps *Deps, projectName, app, replica string) (*ssh.Client, error) {
	project, err := resolveProject(ctx, deps, projectName)
	if err != nil {
		return nil, err
	}
	return exec.Connect(ctx, func(ctx context.Context, publicKey string) (*client.ExecSession, error) {
		return deps.Client.IssueExecSession(ctx, project.ID, app, client.ExecSessionRequest{PublicKey: publicKey, Replica: replica})
	})
}

func appExecCmd(deps *Deps) *cobra.Command {
	var project, replica string
	c := &cobra.Command{
		Use:   "exec <app> --project <project> [--replica <id>] -- <command> [args...]",
		Short: "Run one command in a running app",
		Long: `Run one command in a running app VM, as the app's own user, environment and working
directory. stdin, stdout and stderr are passed through unchanged and heroctl exits with the
command's exit code.

` + execHelpNote,
		Example: "  heroctl exec web --project shop -- ls -l /app",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 1 || len(args) < 2 {
				return errors.New("usage: heroctl exec <app> --project <project> -- <command> [args...]")
			}
			return runRemote(cmd, deps, project, args[0], replica, args[1:])
		},
	}
	c.Flags().StringVar(&project, "project", "", "Project name (required)")
	c.Flags().StringVar(&replica, "replica", "", "Replica (allocation ID) when several are running")
	_ = c.MarkFlagRequired("project")
	return c
}

func appConsoleCmd(deps *Deps) *cobra.Command {
	var project, replica string
	c := &cobra.Command{
		Use:   "console <app> --project <project> [--replica <id>]",
		Short: "Open an interactive shell in a running app",
		Long: `Open an interactive shell in a running app VM, as the app's own user, environment and
working directory. Images without a shell cannot open a console; use exec instead.

` + execHelpNote,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return openConsole(cmd, deps, project, args[0], replica, nil)
		},
	}
	c.Flags().StringVar(&project, "project", "", "Project name (required)")
	c.Flags().StringVar(&replica, "replica", "", "Replica (allocation ID) when several are running")
	_ = c.MarkFlagRequired("project")
	return c
}

// runRemote runs argv in the app and turns the remote exit code into an ExitCodeError.
func runRemote(cmd *cobra.Command, deps *Deps, project, app, replica string, argv []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	conn, err := connectApp(ctx, deps, project, app, replica)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	code, err := exec.Run(ctx, conn, argv, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	if code != 0 {
		return &ExitCodeError{Code: code}
	}
	return nil
}

// openConsole opens an interactive session in the app on the local terminal. A nil argv
// runs the login shell, otherwise argv runs on the PTY.
func openConsole(cmd *cobra.Command, deps *Deps, project, app, replica string, argv []string) error {
	term, err := openTerminal()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	conn, err := connectApp(ctx, deps, project, app, replica)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	termName := os.Getenv("TERM")
	if termName == "" {
		termName = "xterm"
	}
	code, err := exec.Console(ctx, conn, term, termName, argv, cmd.InOrStdin(), cmd.OutOrStdout())
	if err != nil {
		return err
	}
	if code != 0 {
		return &ExitCodeError{Code: code}
	}
	return nil
}
