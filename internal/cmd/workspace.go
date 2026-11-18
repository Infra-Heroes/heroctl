package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/workspace"
)

// workspaceCmd manages named contexts: a server, a login and a default project.
func workspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Switch between logins, servers and default projects",
		Long: `A workspace is a named context: which server heroctl talks to, which login it
uses, and which project a command uses when --project is not given.

  heroctl workspace create acme --project shop     # a second login, project "shop"
  heroctl workspace use acme && heroctl login       # switch to it and sign in
  heroctl deployments list                          # runs in acme / shop
  heroctl --workspace default deployments list      # one command in another one

HERO_WORKSPACE selects one for a whole shell or CI job. The login you had before
workspaces existed is the workspace "default".`,
	}
	cmd.AddCommand(workspaceListCmd(), workspaceCreateCmd(), workspaceUseCmd(),
		workspaceCurrentCmd(), workspaceSetCmd(), workspaceDeleteCmd())
	return cmd
}

func workspaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workspaces; the active one is marked",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := workspace.Load()
			if err != nil {
				return err
			}
			active, _, err := f.Active(os.Getenv("HERO_WORKSPACE"))
			if err != nil {
				active = ""
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "\tNAME\tPROJECT\tSERVER\tLOGIN")
			for _, n := range f.Names() {
				ws := f.Workspaces[n]
				mark := ""
				if n == active {
					mark = "*"
				}
				login := "no"
				if p, err := workspace.TokenFile(n); err == nil {
					if _, err := os.Stat(p); err == nil {
						login = "yes"
					}
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", mark, n, orDash(ws.Project), orDash(ws.Server), login)
			}
			return w.Flush()
		},
	}
}

func workspaceCreateCmd() *cobra.Command {
	var ws workspace.Workspace
	var use bool
	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a workspace (then: heroctl workspace use NAME && heroctl login)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := workspace.ValidName(name); err != nil {
				return err
			}
			f, err := workspace.Load()
			if err != nil {
				return err
			}
			if _, ok := f.Workspaces[name]; ok {
				return fmt.Errorf("workspace %q exists already", name)
			}
			f.Workspaces[name] = ws
			if use {
				f.Current = name
			}
			if err := f.Save(); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created workspace %q.", name)
			if use {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), " It is active; sign in with: heroctl login")
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			return nil
		},
	}
	cmd.Flags().StringVar(&ws.Project, "project", "", "Project used when a command is not given --project")
	cmd.Flags().StringVar(&ws.Server, "server", "", "hero-api URL (default: the built-in one)")
	cmd.Flags().BoolVar(&use, "use", false, "Make it the active workspace")
	return cmd
}

func workspaceUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use NAME",
		Short: "Make a workspace the active one",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := workspace.Load()
			if err != nil {
				return err
			}
			if _, ok := f.Workspaces[args[0]]; !ok {
				return fmt.Errorf("workspace %q does not exist (heroctl workspace list)", args[0])
			}
			f.Current = args[0]
			if err := f.Save(); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Now using workspace %q.\n", args[0])
			return nil
		},
	}
}

func workspaceCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Print the active workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := workspace.Load()
			if err != nil {
				return err
			}
			name, ws, err := f.Active(os.Getenv("HERO_WORKSPACE"))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s (project %s, server %s)\n", name, orDash(ws.Project), orDash(ws.Server))
			return nil
		},
	}
}

func workspaceSetCmd() *cobra.Command {
	var project, server string
	cmd := &cobra.Command{
		Use:   "set NAME",
		Short: "Change a workspace's default project or server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := workspace.Load()
			if err != nil {
				return err
			}
			ws, ok := f.Workspaces[args[0]]
			if !ok {
				return fmt.Errorf("workspace %q does not exist", args[0])
			}
			if cmd.Flags().Changed("project") {
				ws.Project = project
			}
			if cmd.Flags().Changed("server") {
				ws.Server = server
			}
			f.Workspaces[args[0]] = ws
			return f.Save()
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Default project (empty clears it)")
	cmd.Flags().StringVar(&server, "server", "", "hero-api URL (empty: the built-in one)")
	return cmd
}

func workspaceDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a workspace and its login",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if name == workspace.Default {
				return fmt.Errorf("the default workspace cannot be deleted")
			}
			f, err := workspace.Load()
			if err != nil {
				return err
			}
			if _, ok := f.Workspaces[name]; !ok {
				return fmt.Errorf("workspace %q does not exist", name)
			}
			delete(f.Workspaces, name)
			if f.Current == name {
				f.Current = ""
			}
			if err := f.Save(); err != nil {
				return err
			}
			if p, err := workspace.TokenFile(name); err == nil {
				_ = os.Remove(p)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted workspace %q.\n", name)
			return nil
		},
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
