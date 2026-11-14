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

func deploymentsMetricsCmd(deps *Deps) *cobra.Command {
	var projectName, rangeKey string
	cmd := &cobra.Command{
		Use:   "metrics <app>",
		Short: "Show CPU, memory and network usage of a deployment",
		Long: `Shows what a deployment used over a range: the latest value, the average and
the highest, and a small chart of the course. The usage is summed over all
running instances.

  heroctl deployments metrics my-app --project my-project
  heroctl deployments metrics my-app --project my-project --range 24h

Ranges: 1h, 6h, 24h, 7d, 30d.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			m, err := deps.Client.GetDeploymentMetrics(ctx, project.ID, args[0], rangeKey)
			if err != nil {
				return fmt.Errorf("metrics: %w", err)
			}
			printMetrics(args[0], m)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&rangeKey, "range", "", "How far back: 1h, 6h, 24h, 7d or 30d (default 1h)")
	return cmd
}

func printMetrics(app string, m *client.DeploymentMetrics) {
	fmt.Printf("%s over the last %s, %d running instance(s)\n\n", app, m.Range, m.Allocations)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "METRIC\tNOW\tAVERAGE\tMAX\tCOURSE")
	for _, s := range m.Series {
		if len(s.Points) == 0 {
			_, _ = fmt.Fprintf(tw, "%s\t-\t-\t-\tno data yet\n", s.Name)
			continue
		}
		vals := make([]float64, len(s.Points))
		var sum, max float64
		for i, p := range s.Points {
			vals[i] = p.V
			sum += p.V
			if p.V > max {
				max = p.V
			}
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.Name,
			formatMetric(vals[len(vals)-1], s.Unit), formatMetric(sum/float64(len(vals)), s.Unit),
			formatMetric(max, s.Unit), sparkline(vals, 24))
	}
	_ = tw.Flush()
}

// formatMetric writes a value with the unit the API names for it.
func formatMetric(v float64, unit string) string {
	switch unit {
	case "percent":
		return fmt.Sprintf("%.1f %%", v)
	case "bytes":
		return humanBytes(v)
	case "bytes_per_second":
		return humanBytes(v) + "/s"
	}
	return fmt.Sprintf("%.2f", v)
}

func humanBytes(b float64) string {
	const k = 1024.0
	switch {
	case b >= k*k*k:
		return fmt.Sprintf("%.1f GiB", b/(k*k*k))
	case b >= k*k:
		return fmt.Sprintf("%.1f MiB", b/(k*k))
	case b >= k:
		return fmt.Sprintf("%.1f KiB", b/k)
	}
	return fmt.Sprintf("%.0f B", b)
}

// sparkline draws values as a row of block characters, scaled between the
// lowest and the highest, shortened to at most width by averaging buckets.
func sparkline(vals []float64, width int) string {
	if len(vals) == 0 || width <= 0 {
		return ""
	}
	if len(vals) > width {
		out := make([]float64, width)
		for i := range out {
			lo, hi := i*len(vals)/width, (i+1)*len(vals)/width
			var sum float64
			for _, v := range vals[lo:hi] {
				sum += v
			}
			out[i] = sum / float64(hi-lo)
		}
		vals = out
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, v := range vals {
		i := 0
		if hi > lo {
			i = int((v - lo) / (hi - lo) * float64(len(blocks)-1))
		}
		b.WriteRune(blocks[i])
	}
	return b.String()
}

func deploymentsEventsCmd(deps *Deps) *cobra.Command {
	var projectName string
	var limit int
	cmd := &cobra.Command{
		Use:   "events <app>",
		Short: "List what happened to a deployment: starts, stops, restarts, resizes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			got, err := deps.Client.ListDeploymentEvents(ctx, project.ID, args[0], limit)
			if err != nil {
				return fmt.Errorf("events: %w", err)
			}
			if len(got.Events) == 0 {
				fmt.Printf("%s has no recorded events yet.\n", args[0])
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "WHEN\tACTION\tOUTCOME\tBY\tDETAIL")
			for _, e := range got.Events {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", when(e.CreatedAt), e.Action, e.Outcome, dash(e.Actor), dash(e.Detail))
			}
			_ = tw.Flush()
			if int(got.Total) > len(got.Events) {
				fmt.Printf("\nShowing %d of %d. More with --limit.\n", len(got.Events), got.Total)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().IntVar(&limit, "limit", 20, "How many events to show (newest first)")
	return cmd
}

// requestNoise is the audit action the API writes for every call it answers. It
// has no target and no detail and, next to the entries that say what changed, it
// buries them: a list of the last twenty is nineteen of these and one deploy.
const requestNoise = "api.request"

// auditPageSize is how many entries one request fetches while looking for
// enough that are not noise.
const auditPageSize = 100

// auditMaxPages bounds that search: the log is long and mostly noise.
const auditMaxPages = 10

func auditCmd(deps *Deps) *cobra.Command {
	var limit int
	var all bool
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show the audit log of your organisation",
		Long: `Lists who did what in your organisation, newest first: deploys, secret and
member changes, volume actions. Actions by the platform operator are shown as such.

The API also records every request it answers (api.request). They say nothing
about what changed and are left out; --all shows them as well.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := collectAudit(cmd.Context(), deps, limit, all)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("Nothing to show in the audit log.")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "WHEN\tACTION\tBY\tTARGET\tDETAIL")
			for _, e := range entries {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", when(e.CreatedAt), e.Action, dash(e.Actor), dash(e.Target), dash(e.Detail))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "How many entries to show (newest first)")
	cmd.Flags().BoolVar(&all, "all", false, "Include the per-request entries (api.request)")
	return cmd
}

// collectAudit reads pages until it has limit entries worth showing, or the log
// or the page budget runs out.
func collectAudit(ctx context.Context, deps *Deps, limit int, all bool) ([]client.AuditEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []client.AuditEntry
	for page := 0; page < auditMaxPages && len(out) < limit; page++ {
		got, err := deps.Client.ListAudit(ctx, auditPageSize, page*auditPageSize)
		if err != nil {
			return nil, fmt.Errorf("audit: %w", err)
		}
		for _, e := range got.Entries {
			if all || e.Action != requestNoise {
				out = append(out, e)
				if len(out) == limit {
					return out, nil
				}
			}
		}
		if len(got.Entries) < auditPageSize {
			break
		}
	}
	return out, nil
}
