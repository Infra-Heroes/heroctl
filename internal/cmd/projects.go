package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func projectsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Manage projects",
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
			fmt.Printf("Project %q created (ID: %s)\n", p.Name, p.ID)
			return nil
		},
	}

	get := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a project by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := resolveProject(cmd.Context(), deps, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Name:      %s\n", p.Name)
			fmt.Printf("ID:        %s\n", p.ID)
			fmt.Printf("Created:   %s\n", p.CreatedAt)
			return nil
		},
	}

	del := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a project by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := resolveProject(cmd.Context(), deps, args[0])
			if err != nil {
				return err
			}
			if err := deps.Client.DeleteProject(cmd.Context(), p.ID); err != nil {
				return fmt.Errorf("delete project: %w", err)
			}
			fmt.Printf("Project %q deleted.\n", p.Name)
			return nil
		},
	}

	cmd.AddCommand(list, create, get, del)
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
	fmt.Fprintln(w, "ID\tNAME\tCREATED")
	for _, p := range projects {
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.ID, p.Name, p.CreatedAt)
	}
	return w.Flush()
}
