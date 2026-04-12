package cmd

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func deploymentsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployments",
		Short: "Manage deployments",
	}

	var listProject string
	list := &cobra.Command{
		Use:   "list",
		Short: "List deployments for a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, listProject)
			if err != nil {
				return err
			}
			ds, err := deps.Client.ListDeployments(cmd.Context(), project.ID)
			if err != nil {
				return fmt.Errorf("list deployments: %w", err)
			}
			if len(ds) == 0 {
				fmt.Println("No deployments for this project.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATUS\tIMAGE\tCPU\tMEM(MB)\tPORT\tCREATED")
			for _, d := range ds {
				fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%d\t%d\t%s\n",
					d.ID, d.Status, d.Image, d.CPU, d.MemoryMB, d.Port, d.CreatedAt)
			}
			return w.Flush()
		},
	}
	list.Flags().StringVar(&listProject, "project", "", "Project name (required)")
	_ = list.MarkFlagRequired("project")

	var getProject string
	get := &cobra.Command{
		Use:   "get <deploymentID>",
		Short: "Get a single deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, getProject)
			if err != nil {
				return err
			}
			deploymentID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid deployment ID %q", args[0])
			}
			d, err := deps.Client.GetDeployment(cmd.Context(), project.ID, deploymentID)
			if err != nil {
				return fmt.Errorf("get deployment: %w", err)
			}
			fmt.Printf("ID:        %d\n", d.ID)
			fmt.Printf("Status:    %s\n", d.Status)
			fmt.Printf("Image:     %s\n", d.Image)
			fmt.Printf("CPU:       %d\n", d.CPU)
			fmt.Printf("Memory MB: %d\n", d.MemoryMB)
			fmt.Printf("Port:      %d\n", d.Port)
			fmt.Printf("Created:   %s\n", d.CreatedAt)
			return nil
		},
	}
	get.Flags().StringVar(&getProject, "project", "", "Project name (required)")
	_ = get.MarkFlagRequired("project")

	var stopProject string
	stop := &cobra.Command{
		Use:   "stop <deploymentID>",
		Short: "Stop a running deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, stopProject)
			if err != nil {
				return err
			}
			deploymentID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid deployment ID %q", args[0])
			}
			if err := deps.Client.StopDeployment(cmd.Context(), project.ID, deploymentID); err != nil {
				return fmt.Errorf("stop deployment: %w", err)
			}
			fmt.Printf("Deployment %d stopped.\n", deploymentID)
			return nil
		},
	}
	stop.Flags().StringVar(&stopProject, "project", "", "Project name (required)")
	_ = stop.MarkFlagRequired("project")

	cmd.AddCommand(list, get, stop)
	return cmd
}
