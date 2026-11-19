package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// teamsCmd: members grouped so a project can be shared with all of them at
// once. Owners and admins manage teams; everyone may list them.
func teamsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "teams",
		Short: "Group members into teams and give teams projects",
	}
	cmd.AddCommand(
		teamsListCmd(deps),
		teamsCreateCmd(deps),
		teamsDeleteCmd(deps),
		teamsAddCmd(deps),
		teamsRemoveCmd(deps),
		teamsGrantCmd(deps),
		teamsRevokeCmd(deps),
	)
	return cmd
}

// resolveTeam finds a team by name or ID.
func resolveTeam(cmd *cobra.Command, deps *Deps, nameOrID string) (*client.Team, error) {
	teams, err := deps.Client.ListTeams(cmd.Context())
	if err != nil {
		return nil, err
	}
	for i := range teams {
		if teams[i].ID == nameOrID || strings.EqualFold(teams[i].Name, nameOrID) {
			return &teams[i], nil
		}
	}
	return nil, fmt.Errorf("no team %q (heroctl teams list)", nameOrID)
}

func teamsListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List teams with their members and projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			teams, err := deps.Client.ListTeams(cmd.Context())
			if err != nil {
				return err
			}
			if len(teams) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No teams yet. Create one with: heroctl teams create <name>")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "TEAM\tMEMBERS\tPROJECTS")
			for _, t := range teams {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", t.Name, teamMembersText(t), teamProjectsText(t))
			}
			return w.Flush()
		},
	}
}

func teamMembersText(t client.Team) string {
	if len(t.Members) == 0 {
		return "-"
	}
	out := make([]string, len(t.Members))
	for i, m := range t.Members {
		out[i] = m.Email
	}
	return strings.Join(out, ", ")
}

func teamProjectsText(t client.Team) string {
	if len(t.Projects) == 0 {
		return "-"
	}
	out := make([]string, len(t.Projects))
	for i, p := range t.Projects {
		out[i] = p.ProjectName + " (" + p.Role + ")"
	}
	return strings.Join(out, ", ")
}

func teamsCreateCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := deps.Client.CreateTeam(cmd.Context(), args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Team %s created. Add people with: heroctl teams add %s <email>\n", args[0], args[0])
			return nil
		},
	}
}

func teamsDeleteCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <team>",
		Short: "Delete a team; its members lose the projects they had through it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveTeam(cmd, deps, args[0])
			if err != nil {
				return err
			}
			if err := deps.Client.DeleteTeam(cmd.Context(), t.ID); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Team %s deleted.\n", t.Name)
			return nil
		},
	}
}

func teamsMemberCmd(deps *Deps, use, short string, in bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <team> <email|principalID>",
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveTeam(cmd, deps, args[0])
			if err != nil {
				return err
			}
			m, err := resolveMember(cmd, deps, args[1])
			if err != nil {
				return err
			}
			if err := deps.Client.SetTeamMember(cmd.Context(), t.ID, m.PrincipalID, in); err != nil {
				return err
			}
			verb := "added to"
			if !in {
				verb = "removed from"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s team %s.\n", m.Email, verb, t.Name)
			return nil
		},
	}
}

func teamsAddCmd(deps *Deps) *cobra.Command {
	return teamsMemberCmd(deps, "add", "Add a member to a team", true)
}

func teamsRemoveCmd(deps *Deps) *cobra.Command {
	return teamsMemberCmd(deps, "remove", "Remove a member from a team", false)
}

func teamsGrantCmd(deps *Deps) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "grant <team> <project>",
		Short: "Give a team a project (--role editor or viewer)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if role != "editor" && role != "viewer" {
				return fmt.Errorf("--role must be editor or viewer")
			}
			t, err := resolveTeam(cmd, deps, args[0])
			if err != nil {
				return err
			}
			p, err := resolveProject(cmd.Context(), deps, args[1])
			if err != nil {
				return err
			}
			if err := deps.Client.SetTeamProject(cmd.Context(), t.ID, p.ID, role); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Team %s is now %s on project %s.\n", t.Name, role, p.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", "viewer", "editor (may change) or viewer (may only look)")
	return cmd
}

func teamsRevokeCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <team> <project>",
		Short: "Take a project away from a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveTeam(cmd, deps, args[0])
			if err != nil {
				return err
			}
			p, err := resolveProject(cmd.Context(), deps, args[1])
			if err != nil {
				return err
			}
			if err := deps.Client.SetTeamProject(cmd.Context(), t.ID, p.ID, ""); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Team %s no longer has project %s.\n", t.Name, p.Name)
			return nil
		},
	}
}
