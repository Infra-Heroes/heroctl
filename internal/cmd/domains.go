package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func domainsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domains",
		Short: "Bring your own domain: the DNS records and the ownership check",
		Long: `Bring your own domain in three steps.

1. Point it at the platform with a CNAME (a subdomain such as www.example.org,
   not the bare example.org: an apex cannot be a CNAME, and an A record would
   pin the load balancer's IP):

     www.example.org   CNAME   ingress.infra-heroes.de.

   Mind the dot at the end: without it many DNS providers append the zone.

2. List it in hero.toml: custom_domains = ["www.example.org"]. The certificate
   comes from Let's Encrypt on its own, over port 443 (TLS-ALPN-01).

3. Optional, and recommended: prove that you control it. Set verify_domains =
   true under [app]; a deploy then requires a TXT record for every domain:

     _hero-challenge.<domain>   TXT   hero-verification=<token>

'heroctl domains challenge' shows both records, 'heroctl domains verify' checks
the TXT record and where the CNAME points. The TXT record only proves
ownership; Let's Encrypt does not read it.`,
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
			printChallenge(cmd.OutOrStdout(), c)
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
		Short: "Check the TXT record and the CNAME of a domain now",
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
			out := cmd.OutOrStdout()
			printChallenge(out, c)
			printCNAMECheck(ctx, out, args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

// IngressHost is where every custom domain points, by CNAME.
const IngressHost = "ingress.infra-heroes.de."

// lookupCNAME is the resolver, replaceable in tests.
var lookupCNAME = net.DefaultResolver.LookupCNAME

// isApex reports a bare registrable domain (example.org): it cannot carry a
// CNAME. Two-part suffixes such as example.co.uk are not caught.
func isApex(domain string) bool {
	return len(strings.Split(strings.Trim(domain, "."), ".")) < 3
}

func printChallenge(w io.Writer, c *client.DomainChallenge) {
	_, _ = fmt.Fprintf(w, "Point it at the platform (if not done yet):\n\n  CNAME  %s  %s\n\n", c.Domain, IngressHost)
	if c.Verified {
		_, _ = fmt.Fprintf(w, "%s is verified.\n", c.Domain)
		return
	}
	_, _ = fmt.Fprintf(w, "%s is not verified yet. Add this record at your DNS provider:\n\n", c.Domain)
	_, _ = fmt.Fprintf(w, "  %s  %s  %s\n\n", c.RecordType, c.RecordName, c.RecordValue)
	_, _ = fmt.Fprintf(w, "then run: heroctl domains verify %s --project <project>\n", c.Domain)
}

// printCNAMECheck says where the domain points now, as this machine sees it.
func printCNAMECheck(ctx context.Context, w io.Writer, domain string) {
	if isApex(domain) {
		_, _ = fmt.Fprintf(w, "\n%s is a bare domain and cannot be a CNAME. Use a subdomain such as www.%s.\n", domain, domain)
		return
	}
	got, err := lookupCNAME(ctx, domain)
	switch {
	case err != nil:
		_, _ = fmt.Fprintf(w, "\nCNAME: %s does not resolve yet (%v).\n", domain, err)
	case strings.EqualFold(got, IngressHost):
		_, _ = fmt.Fprintf(w, "\nCNAME: %s points at %s. Good.\n", domain, IngressHost)
	case strings.HasPrefix(strings.ToLower(got), IngressHost):
		// ingress.infra-heroes.de.example.org.: the provider appended the zone.
		_, _ = fmt.Fprintf(w, "\nCNAME: %s points at %s: the dot at the end of %s is missing at your DNS provider.\n", domain, got, IngressHost)
	default:
		_, _ = fmt.Fprintf(w, "\nCNAME: %s points at %s, not at %s.\n", domain, got, IngressHost)
	}
}
