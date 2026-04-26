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

	projectMembersCmd := projectsMembersCmd(deps)
	cmd.AddCommand(list, create, get, del, projectMembersCmd)
	return cmd
}

func projectsMembersCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Manage project-level members",
	}

	list := &cobra.Command{
		Use:   "list <project-name>",
		Short: "List members of a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := resolveProject(cmd.Context(), deps, args[0])
			if err != nil {
				return err
			}
			members, err := deps.Client.ListProjectMembers(cmd.Context(), p.ID)
			if err != nil {
				return err
			}
			if len(members) == 0 {
				fmt.Println("No explicit project members (owner/admin have implicit access).")
				return nil
			}
			fmt.Printf("%-42s  %-8s\n", "PRINCIPAL ID", "ROLE")
			for _, m := range members {
				fmt.Printf("%-42s  %-8s\n", m.PrincipalID, m.Role)
			}
			return nil
		},
	}

	var role string
	add := &cobra.Command{
		Use:   "add <project-name> <principalID>",
		Short: "Add or update a project member",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := resolveProject(cmd.Context(), deps, args[0])
			if err != nil {
				return err
			}
			if err := deps.Client.UpsertProjectMember(cmd.Context(), p.ID, args[1], role); err != nil {
				return err
			}
			fmt.Printf("Member %s added to project %q with role %s.\n", args[1], p.Name, role)
			return nil
		},
	}
	add.Flags().StringVar(&role, "role", "viewer", "Project role: 'editor' or 'viewer'")

	remove := &cobra.Command{
		Use:   "remove <project-name> <principalID>",
		Short: "Remove a project member",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := resolveProject(cmd.Context(), deps, args[0])
			if err != nil {
				return err
			}
			if err := deps.Client.RemoveProjectMember(cmd.Context(), p.ID, args[1]); err != nil {
				return err
			}
			fmt.Printf("Member %s removed from project %q.\n", args[1], p.Name)
			return nil
		},
	}

	cmd.AddCommand(list, add, remove)
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
	_, _ = fmt.Fprintln(w, "ID\tNAME\tCREATED")
	for _, p := range projects {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", p.ID, p.Name, p.CreatedAt)
	}
	return w.Flush()
}
