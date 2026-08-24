package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Infra-Heroes/heroctl/internal/build"
	"github.com/Infra-Heroes/heroctl/internal/client"
)

func signupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "signup",
		Short: "Create a new account",
		RunE: func(cmd *cobra.Command, args []string) error {
			if build.ServerURL == "" {
				return fmt.Errorf("binary is not configured; build with -ldflags to set ServerURL")
			}

			r := bufio.NewReader(os.Stdin)

			fmt.Print("Email:    ")
			email, _ := r.ReadString('\n')
			email = strings.TrimSpace(email)

			fmt.Print("Username: ")
			username, _ := r.ReadString('\n')
			username = strings.TrimSpace(username)

			fmt.Print("Org name: ")
			orgName, _ := r.ReadString('\n')
			orgName = strings.TrimSpace(orgName)

			fmt.Print("Password: ")
			passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password := strings.TrimSpace(string(passwordBytes))

			fmt.Print("Invite code: ")
			inviteCode, _ := r.ReadString('\n')
			inviteCode = strings.TrimSpace(inviteCode)

			c := client.New(build.ServerURL, "", "", nil)
			resp, err := c.Signup(cmd.Context(), client.SignupRequest{
				Email:      email,
				Username:   username,
				Password:   password,
				OrgName:    orgName,
				InviteCode: inviteCode,
			})
			if err != nil {
				return fmt.Errorf("signup: %w", err)
			}

			fmt.Printf("\nAccount created successfully.\n")
			fmt.Printf("Org:     %s (ID: %s)\n", resp.Org.Name, resp.Org.ID)
			fmt.Printf("Project: %s (ID: %s)\n", resp.Project.Name, resp.Project.ID)
			fmt.Println("\nRun 'heroctl login' to authenticate.")
			return nil
		},
	}
}
