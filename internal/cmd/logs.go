package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func logsCmd(deps *Deps) *cobra.Command {
	var project string
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs <app>",
		Short: "Stream logs for a running deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			proj, err := resolveProject(cmd.Context(), deps, project)
			if err != nil {
				return err
			}
			body, err := deps.Client.StreamLogs(cmd.Context(), proj.ID, args[0], follow)
			if err != nil {
				return fmt.Errorf("logs: %w", err)
			}
			defer func() { _ = body.Close() }()

			// Copy raw bytes rather than scanning lines. bufio.Scanner caps a
			// token at 64KB and aborts the whole stream with ErrTooLong on a
			// longer line, so a single large log entry (a JSON dump, a stack
			// trace) would suppress all output, including the lines after it.
			if _, err := io.Copy(cmd.OutOrStdout(), body); err != nil {
				return fmt.Errorf("stream logs: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	return cmd
}
