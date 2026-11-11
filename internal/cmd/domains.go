package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func domainsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domains",
		Short: "Prove that you control a custom domain (optional DNS TXT verification)",
		Long: `Custom domains are routed without any proof of ownership unless you ask for it.

To ask, set verify_domains = true under [app] in hero.toml. A deploy then
requires a TXT record for every custom domain:

  _hero-challenge.<domain>   TXT   hero-verification=<token>

'heroctl domains challenge' shows the record, 'heroctl domains verify' checks it
now. Apex and www are separate domains with their own record.`,
	}
	cmd.AddCommand(domainsChallengeCmd(deps), domainsVerifyCmd(deps))
	return cmd
}

func domainsChallengeCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "challenge <domain>",
		Short: "Show the TXT record that proves you control a domain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			c, err := deps.Client.DomainChallenge(ctx, project.ID, args[0])
			if err != nil {
				return fmt.Errorf("domain challenge: %w", err)
			}
			printChallenge(c)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func domainsVerifyCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "verify <domain>",
		Short: "Look the TXT record up now and report whether the domain is verified",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			c, err := deps.Client.VerifyDomain(ctx, project.ID, args[0])
			if err != nil {
				return fmt.Errorf("verify domain: %w", err)
			}
			printChallenge(c)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func printChallenge(c *client.DomainChallenge) {
	if c.Verified {
		fmt.Printf("%s is verified.\n", c.Domain)
		return
	}
	fmt.Printf("%s is not verified yet. Add this record at your DNS provider:\n\n", c.Domain)
	fmt.Printf("  %s  %s  %s\n\n", c.RecordType, c.RecordName, c.RecordValue)
	fmt.Printf("then run: heroctl domains verify %s --project <project>\n", c.Domain)
}
