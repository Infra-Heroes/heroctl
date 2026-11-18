package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// notificationsCmd manages where an organisation's alerts go besides email.
func notificationsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "notifications",
		Aliases: []string{"channels"},
		Short:   "Alert channels: Slack, Teams, Discord or a signed webhook",
		Long: `Every alert of your deployments is mailed to the organisation's owners and
admins. A channel posts it to Slack, Microsoft Teams, Discord or your own https
endpoint as well. Only owners and admins can add, test or remove channels.

  heroctl notifications list
  heroctl notifications add ops --kind slack --url https://hooks.slack.com/services/...
  heroctl notifications test ops
  heroctl notifications delete ops

A webhook is signed: X-Hero-Signature is sha256=HMAC(key, "<X-Hero-Timestamp>.<body>").
The key is printed once, when the webhook is added.`,
	}
	cmd.AddCommand(notificationsListCmd(deps), notificationsAddCmd(deps), notificationsTestCmd(deps), notificationsDeleteCmd(deps))
	return cmd
}

func notificationsListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the organisation's alert channels",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := deps.Client.ListNotificationChannels(cmd.Context())
			if err != nil {
				return fmt.Errorf("list channels: %w", err)
			}
			if len(list) == 0 {
				fmt.Println("No alert channels; alerts go out by email. Add one with: heroctl notifications add <name> --kind slack --url <webhook-url>")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tKIND\tLAST DELIVERY")
			for _, c := range list {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Name, c.Kind, lastDelivery(c))
			}
			return tw.Flush()
		},
	}
}

func lastDelivery(c client.NotificationChannel) string {
	switch {
	case c.LastError != "":
		return "failed: " + c.LastError
	case c.LastSentAt != "":
		return "ok " + c.LastSentAt
	}
	return "-"
}

func notificationsAddCmd(deps *Deps) *cobra.Command {
	var kind, url string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add an alert channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch kind {
			case "slack", "teams", "discord", "webhook":
			default:
				return fmt.Errorf("--kind must be slack, teams, discord or webhook")
			}
			if !strings.HasPrefix(url, "https://") {
				return fmt.Errorf("--url must start with https://")
			}
			c, err := deps.Client.CreateNotificationChannel(cmd.Context(), args[0], kind, url)
			if err != nil {
				return fmt.Errorf("add channel: %w", err)
			}
			fmt.Printf("Channel %q (%s) added. Check it with: heroctl notifications test %s\n", c.Name, c.Kind, c.Name)
			if c.SigningKey != "" {
				fmt.Printf("Signing key (shown only now): %s\n", c.SigningKey)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "slack, teams, discord or webhook (required)")
	cmd.Flags().StringVar(&url, "url", "", "The https URL to post to (required)")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

func notificationsTestCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "test <name>",
		Short: "Send a test message to a channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := channelIDs(cmd.Context(), deps, args)
			if err != nil {
				return err
			}
			if err := deps.Client.TestNotificationChannel(cmd.Context(), id[0]); err != nil {
				return fmt.Errorf("test channel: %w", err)
			}
			fmt.Printf("Test message delivered to %q.\n", args[0])
			return nil
		},
	}
}

func notificationsDeleteCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Remove an alert channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := channelIDs(cmd.Context(), deps, args)
			if err != nil {
				return err
			}
			if err := deps.Client.DeleteNotificationChannel(cmd.Context(), id[0]); err != nil {
				return fmt.Errorf("delete channel: %w", err)
			}
			fmt.Printf("Channel %q removed.\n", args[0])
			return nil
		},
	}
}

// channelIDs turns channel names into ids. The name "none" alone means no
// channel at all (email only) and gives an empty list.
func channelIDs(ctx context.Context, deps *Deps, names []string) ([]string, error) {
	if len(names) == 1 && names[0] == "none" {
		return []string{}, nil
	}
	list, err := deps.Client.ListNotificationChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	byName := make(map[string]string, len(list))
	for _, c := range list {
		byName[c.Name] = c.ID
	}
	ids := make([]string, 0, len(names))
	for _, n := range names {
		id, ok := byName[n]
		if !ok {
			return nil, fmt.Errorf("no alert channel named %q; see heroctl notifications list", n)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
