package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func membersCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Manage org members and invitations",
	}
	cmd.AddCommand(
		membersListCmd(deps),
		membersInviteCmd(deps),
		membersRemoveCmd(deps),
		membersSetRoleCmd(deps),
		membersInvitationsCmd(deps),
	)
	return cmd
}

func membersListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List org members",
		RunE: func(cmd *cobra.Command, args []string) error {
			members, err := deps.Client.ListOrgMembers(cmd.Context())
			if err != nil {
				return err
			}
			if len(members) == 0 {
				fmt.Println("No members.")
				return nil
			}
			fmt.Printf("%-42s  %-30s  %-8s\n", "PRINCIPAL ID", "EMAIL", "ROLE")
			fmt.Println(strings.Repeat("-", 86))
			for _, m := range members {
				fmt.Printf("%-42s  %-30s  %-8s\n", m.PrincipalID, m.Email, m.Role)
			}
			return nil
		},
	}
}

func membersInviteCmd(deps *Deps) *cobra.Command {
	var (
		role         string
		projectFlags []string
	)
	cmd := &cobra.Command{
		Use:   "invite <email>",
		Short: "Invite a user to the org by email",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]

			var projectRoles []client.ProjectRoleEntry
			for _, pf := range projectFlags {
				parts := strings.SplitN(pf, ":", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid --project flag %q: expected <project-id>:<role>", pf)
				}
				if parts[1] != "editor" && parts[1] != "viewer" {
					return fmt.Errorf("project role must be 'editor' or 'viewer', got %q", parts[1])
				}
				projectRoles = append(projectRoles, client.ProjectRoleEntry{
					ProjectID: parts[0],
					Role:      parts[1],
				})
			}

			inv, err := deps.Client.CreateInvitation(cmd.Context(), client.CreateInvitationRequest{
				Email:        email,
				OrgRole:      role,
				ProjectRoles: projectRoles,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Invitation created.\n")
			fmt.Printf("  Token:      %s\n", inv.Token)
			fmt.Printf("  Email:      %s\n", inv.Email)
			fmt.Printf("  Role:       %s\n", inv.OrgRole)
			fmt.Printf("  Expires at: %s\n", inv.ExpiresAt)
			fmt.Printf("\nShare this token with %s. They should run:\n  heroctl accept-invite %s\n", inv.Email, inv.Token)
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", "member", "Org role to assign: 'admin' or 'member'")
	cmd.Flags().StringArrayVar(&projectFlags, "project", nil, "Project access as <project-id>:<editor|viewer> (repeatable)")
	return cmd
}

func membersRemoveCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <principalID>",
		Short: "Remove a member from the org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := deps.Client.RemoveOrgMember(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Member %s removed.\n", args[0])
			return nil
		},
	}
}

func membersSetRoleCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "set-role <principalID> <admin|member>",
		Short: "Change an org member's role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[1] != "admin" && args[1] != "member" {
				return fmt.Errorf("role must be 'admin' or 'member'")
			}
			if err := deps.Client.UpdateMemberRole(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("Member %s role set to %s.\n", args[0], args[1])
			return nil
		},
	}
}

func membersInvitationsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invitations",
		Short: "Manage pending invitations",
	}
	cmd.AddCommand(membersInvitationsListCmd(deps), membersInvitationsRevokeCmd(deps))
	return cmd
}

func membersInvitationsListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending invitations",
		RunE: func(cmd *cobra.Command, args []string) error {
			invs, err := deps.Client.ListInvitations(cmd.Context())
			if err != nil {
				return err
			}
			if len(invs) == 0 {
				fmt.Println("No pending invitations.")
				return nil
			}
			fmt.Printf("%-36s  %-30s  %-8s  %s\n", "ID", "EMAIL", "ROLE", "EXPIRES")
			fmt.Println(strings.Repeat("-", 70))
			for _, inv := range invs {
				fmt.Printf("%-36s  %-30s  %-8s  %s\n", inv.ID, inv.Email, inv.OrgRole, inv.ExpiresAt)
			}
			return nil
		},
	}
}

func membersInvitationsRevokeCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a pending invitation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := deps.Client.RevokeInvitation(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Printf("Invitation %s revoked.\n", id)
			return nil
		},
	}
}

// acceptInviteCmd is a top-level command for accepting an invitation token.
// Requires a logged-in session.
func acceptInviteCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "accept-invite <token>",
		Short: "Accept an org membership invitation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := deps.Client.AcceptInvitation(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Println("Invitation accepted. You are now a member of the org.")
			return nil
		},
	}
}
