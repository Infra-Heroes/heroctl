package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func sshCmd(deps *Deps) *cobra.Command {
	var project string
	var runCmd string

	cmd := &cobra.Command{
		Use:   "ssh <app>",
		Short: "Start an interactive shell session in the active deployment container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			proj, err := resolveProject(cmd.Context(), deps, project)
			if err != nil {
				return err
			}

			fmt.Printf("Connecting to shell for app %q in project %q...\n", args[0], proj.Name)
			conn, err := deps.Client.SSHDeployment(cmd.Context(), proj.ID, args[0], runCmd)
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

			errCh := make(chan error, 2)
			go func() {
				_, err := io.Copy(conn, os.Stdin)
				errCh <- err
			}()
			go func() {
				_, err := io.Copy(os.Stdout, conn)
				errCh <- err
			}()

			select {
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case err := <-errCh:
				if err != nil && err != io.EOF {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&runCmd, "cmd", "", "Command to execute (default /bin/sh)")
	return cmd
}
