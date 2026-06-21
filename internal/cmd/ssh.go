package cmd

import (
	"fmt"
	"io"
	"net"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func sshCmd(deps *Deps) *cobra.Command {
	var project string
	var runCmd string

	cmd := &cobra.Command{
		Use:   "ssh <target>",
		Short: "Start an interactive shell session in the active deployment container",
		Long: `Start an interactive shell session.
In direct mode: heroctl ssh <app> --project <project>`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			var conn net.Conn
			var err error

			if project == "" {
				return fmt.Errorf("flag --project is required")
			}
			proj, err := resolveProject(cmd.Context(), deps, project)
			if err != nil {
				return err
			}

			fmt.Printf("Connecting to shell for app %q in project %q...\n", target, proj.Name)
			conn, err = deps.Client.SSHDeployment(cmd.Context(), proj.ID, target, runCmd)
			if err != nil {
				return fmt.Errorf("ssh: %w", err)
			}

			defer func() { _ = conn.Close() }()

			fd := int(os.Stdin.Fd())
			if term.IsTerminal(fd) {
				oldState, err := term.MakeRaw(fd)
				if err != nil {
					return fmt.Errorf("set raw terminal: %w", err)
				}
				defer func() { _ = term.Restore(fd, oldState) }()
			}

			go func() {
				_, _ = io.Copy(conn, cmd.InOrStdin())
			}()

			_, err = io.Copy(cmd.OutOrStdout(), conn)
			if err != nil && err != io.EOF {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Project name (required)")
	cmd.Flags().StringVar(&runCmd, "cmd", "", "Command to execute (default /bin/sh)")
	return cmd
}
