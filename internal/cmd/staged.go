package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Blue/green: "heroctl deploy --stage" starts the new version beside production;
// these commands look at it and decide.

func printStaged(app, project, nextURL string) {
	fmt.Printf("Staged %q beside production. Production is unchanged.\n", app)
	if nextURL != "" {
		fmt.Printf("Try it:    %s\n", nextURL)
	}
	fmt.Printf("Go live:   heroctl deployments promote %s --project %s\n", app, project)
	fmt.Printf("Throw away: heroctl deployments discard %s --project %s\n", app, project)
	fmt.Println("It shares production's database and secrets; undecided after 24 h, it is discarded.")
}

func deploymentsStagedCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "staged <app>",
		Short: "Show the version waiting beside production (heroctl deploy --stage)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			s, err := deps.Client.GetStagedDeployment(ctx, project.ID, args[0])
			if err != nil {
				return fmt.Errorf("staged version of %s: %w", args[0], err)
			}
			fmt.Printf("Waiting:     %s\nProduction:  %s\nURL:         %s\nSince:       %s\n", s.Image, s.LiveImage, s.NextURL, s.Since)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func deploymentsDecideCmd(deps *Deps, decision, short, done string) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   decision + " <app>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			if err := deps.Client.DecideStagedDeployment(ctx, project.ID, args[0], decision); err != nil {
				return fmt.Errorf("%s %s: %w", decision, args[0], err)
			}
			fmt.Printf(done+"\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
