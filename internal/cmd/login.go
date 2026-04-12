package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/auth"
	"github.com/Infra-Heroes/heroctl/internal/build"
)

func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate via device code flow",
		RunE: func(cmd *cobra.Command, args []string) error {
			if build.AuthDomain == "" || build.ClientID == "" {
				return fmt.Errorf("binary is not configured — build with -ldflags to set AuthDomain and ClientID")
			}

			ctx := cmd.Context()

			dar, err := auth.StartDeviceFlow(ctx, build.AuthDomain, build.ClientID)
			if err != nil {
				return fmt.Errorf("start device flow: %w", err)
			}

			fmt.Printf("\nOpen the following URL in your browser:\n\n  %s\n\n", dar.VerificationURI)
			fmt.Printf("Enter code: %s\n\n", dar.UserCode)
			fmt.Println("Waiting for authentication...")

			// Best-effort browser open — ignore failures.
			_ = exec.CommandContext(ctx, "xdg-open", dar.VerificationURIComplete).Start()

			expires := time.Duration(dar.ExpiresIn) * time.Second
			pollCtx, cancel := context.WithTimeout(ctx, expires)
			defer cancel()

			tok, err := auth.PollToken(pollCtx, build.AuthDomain, build.ClientID, dar.DeviceCode, dar.Interval)
			if err != nil {
				return err
			}

			if err := auth.Save(tok); err != nil {
				return fmt.Errorf("save token: %w", err)
			}

			fmt.Println("Successfully logged in.")
			return nil
		},
	}
}
