package cmd

import (
	"bufio"
	"fmt"

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
			defer body.Close()

			scanner := bufio.NewScanner(body)
			for scanner.Scan() {
				fmt.Println(scanner.Text())
			}
			return scanner.Err()
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	return cmd
}
