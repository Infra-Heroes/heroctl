package cmd

import (
	"fmt"
	"strconv"
	"text/tabwriter"
	"os"

	"github.com/spf13/cobra"
)

func projectsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Manage projects",
		// Default: list
		RunE: func(cmd *cobra.Command, args []string) error {
			return listProjects(cmd, deps)
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listProjects(cmd, deps)
		},
	}

	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a project (provisions overlay network)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := deps.Client.CreateProject(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("create project: %w", err)
			}
			fmt.Printf("Project %q created (ID: %d, VNI: %d)\n", p.Name, p.ID, p.VNI)
			return nil
		},
	}

	del := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a project by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid project ID %q", args[0])
			}
			if err := deps.Client.DeleteProject(cmd.Context(), id); err != nil {
				return fmt.Errorf("delete project: %w", err)
			}
			fmt.Printf("Project %d deleted.\n", id)
			return nil
		},
	}

	cmd.AddCommand(list, create, del)
	return cmd
}

func listProjects(cmd *cobra.Command, deps *Deps) error {
	projects, err := deps.Client.ListProjects(cmd.Context())
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	if len(projects) == 0 {
		fmt.Println("No projects. Create one with: heroctl projects create <name>")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tVNI\tCREATED")
	for _, p := range projects {
		fmt.Fprintf(w, "%d\t%s\t%d\t%s\n", p.ID, p.Name, p.VNI, p.CreatedAt)
	}
	return w.Flush()
}
