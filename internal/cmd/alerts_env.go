package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// ── alerts ───────────────────────────────────────────────────────────────────

const mib = 1024 * 1024

func deploymentsAlertsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Rules that notify you when a deployment is under strain or down",
		Long: `An alert watches one value of a deployment and notifies you when it stays past
a threshold for a while, or when the deployment has no healthy instance.

  heroctl deployments alerts list my-app --project my-project
  heroctl deployments alerts set my-app --project my-project --metric cpu_percent --threshold 80 --for 300
  heroctl deployments alerts set my-app --project my-project --metric memory_bytes --threshold 400
  heroctl deployments alerts set my-app --project my-project --metric unavailable
  heroctl deployments alerts delete my-app <id> --project my-project

Metrics: cpu_percent (a percentage), memory_bytes (give the threshold in
megabytes), unavailable (no healthy instance; no threshold).`,
	}
	cmd.AddCommand(alertsListCmd(deps), alertsSetCmd(deps), alertsDeleteCmd(deps))
	return cmd
}

func alertsListCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "list <app>",
		Short: "List the alert rules of a deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			alerts, err := deps.Client.ListDeploymentAlerts(ctx, project.ID, args[0])
			if err != nil {
				return fmt.Errorf("list alerts: %w", err)
			}
			if len(alerts) == 0 {
				fmt.Printf("%s has no alerts. Add one with: heroctl deployments alerts set %s --project %s --metric cpu_percent --threshold 80\n", args[0], args[0], projectName)
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "ID\tMETRIC\tTHRESHOLD\tFOR\tENABLED\tSTATE")
			for _, a := range alerts {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\t%s\n", shortID(a.ID), a.Metric, alertThreshold(a), forDuration(a.ForSeconds), a.Enabled, dash(a.State))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func alertThreshold(a client.DeploymentAlert) string {
	switch a.Metric {
	case "cpu_percent":
		return fmt.Sprintf("%.0f %%", a.Threshold)
	case "memory_bytes":
		return fmt.Sprintf("%.0f MB", a.Threshold/mib)
	}
	return "-"
}

func forDuration(seconds int32) string {
	if seconds <= 0 {
		return "-"
	}
	if seconds%60 == 0 {
		return fmt.Sprintf("%d min", seconds/60)
	}
	return fmt.Sprintf("%d s", seconds)
}

func alertsSetCmd(deps *Deps) *cobra.Command {
	var projectName, metric string
	var threshold float64
	var forSeconds int
	var off bool
	cmd := &cobra.Command{
		Use:   "set <app>",
		Short: "Create or change the alert for one metric",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			req, err := buildAlertRequest(metric, threshold, cmd.Flags().Changed("threshold"),
				forSeconds, cmd.Flags().Changed("for"), off, cmd.Flags().Changed("off"))
			if err != nil {
				return err
			}
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			got, err := deps.Client.PutDeploymentAlert(ctx, project.ID, args[0], req)
			if err != nil {
				return fmt.Errorf("save alert: %w", err)
			}
			state := "on"
			if !got.Enabled {
				state = "off"
			}
			fmt.Printf("Alert %s saved for %q (%s, %s).\n", got.Metric, args[0], alertThreshold(*got), state)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&metric, "metric", "", "cpu_percent, memory_bytes or unavailable (required)")
	_ = cmd.MarkFlagRequired("metric")
	cmd.Flags().Float64Var(&threshold, "threshold", 0, "cpu_percent: 1-100; memory_bytes: megabytes")
	cmd.Flags().IntVar(&forSeconds, "for", 0, "Seconds the value must stay past the threshold before you are notified")
	cmd.Flags().BoolVar(&off, "off", false, "Switch the alert off, keeping it")
	return cmd
}

// buildAlertRequest turns the flags into what the API takes. The memory
// threshold is asked for in megabytes, which is what a person thinks in, and
// sent in bytes, which is what the API measures in.
func buildAlertRequest(metric string, threshold float64, hasThreshold bool, forSeconds int, hasFor, off, hasOff bool) (client.DeploymentAlertRequest, error) {
	req := client.DeploymentAlertRequest{Metric: metric}
	switch metric {
	case "cpu_percent":
		if !hasThreshold || threshold <= 0 || threshold > 100 {
			return req, fmt.Errorf("--threshold for cpu_percent must be between 1 and 100")
		}
		req.Threshold = threshold
	case "memory_bytes":
		if !hasThreshold || threshold <= 0 {
			return req, fmt.Errorf("--threshold for memory_bytes is the number of megabytes and must be above 0")
		}
		req.Threshold = threshold * mib
	case "unavailable":
		if hasThreshold {
			return req, fmt.Errorf("unavailable has no threshold: it fires when no instance is healthy")
		}
	default:
		return req, fmt.Errorf("unknown metric %q: use cpu_percent, memory_bytes or unavailable", metric)
	}
	if hasFor {
		if forSeconds < 0 {
			return req, fmt.Errorf("--for cannot be negative")
		}
		v := int32(forSeconds) // #nosec G115 -- a duration in seconds, far below int32
		req.ForSeconds = &v
	}
	if hasOff {
		enabled := !off
		req.Enabled = &enabled
	}
	return req, nil
}

func alertsDeleteCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "delete <app> <id>",
		Short: "Remove an alert rule; the id is the one from the list, or its last characters",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			alerts, err := deps.Client.ListDeploymentAlerts(ctx, project.ID, args[0])
			if err != nil {
				return fmt.Errorf("list alerts: %w", err)
			}
			ids := make([]string, len(alerts))
			for i, a := range alerts {
				ids[i] = a.ID
			}
			id, err := matchEnd(ids, args[1], "alert")
			if err != nil {
				return err
			}
			if err := deps.Client.DeleteDeploymentAlert(ctx, project.ID, args[0], id); err != nil {
				return fmt.Errorf("delete alert: %w", err)
			}
			fmt.Printf("Alert %s removed.\n", shortID(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

// matchEnd finds the one id that ends with want (or equals it): ids are
// time-ordered, so the end is what tells them apart.
func matchEnd(ids []string, want, what string) (string, error) {
	want = strings.ToLower(want)
	if len(want) < minRevisionSuffix {
		return "", fmt.Errorf("%s id %q is too short: give at least %d characters from the end of it", what, want, minRevisionSuffix)
	}
	var found []string
	for _, id := range ids {
		if id == want || strings.HasSuffix(id, want) {
			found = append(found, id)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no %s ends with %q", what, want)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("%d %ss end with %q; give more of the id", len(found), what, want)
}

// ── environment ──────────────────────────────────────────────────────────────

// maskedValue is what the server shows for a variable of a deployment that
// predates stored references: the key is known, the value is not.
const maskedValue = "***"

func deploymentsEnvCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Show or change the environment variables of a deployment",
		Long: `Variables are normally set in hero.toml and applied by a deploy. These commands
change them on a deployment that is already running, which redeploys it with the
new environment.

  heroctl deployments env list my-app --project my-project
  heroctl deployments env set my-app LOG_LEVEL=debug API_URL=https://x.example --project my-project
  heroctl deployments env set my-app DB_PASSWORD=secret:DB_PASSWORD --project my-project
  heroctl deployments env unset my-app LOG_LEVEL --project my-project

A value of the form secret:NAME is replaced by the project secret NAME at deploy
time (see: heroctl secrets). Values are shown as they were set, so secrets appear
as their reference and never as the secret itself.`,
	}
	cmd.AddCommand(envListCmd(deps), envSetCmd(deps), envUnsetCmd(deps))
	return cmd
}

func envListCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "list <app>",
		Short: "List the environment variables of a deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			d, err := deps.Client.GetDeployment(ctx, project.ID, args[0])
			if err != nil {
				return fmt.Errorf("get deployment: %w", err)
			}
			if len(d.Env) == 0 {
				fmt.Printf("%s has no environment variables.\n", args[0])
				return nil
			}
			for _, k := range sortedKeys(d.Env) {
				fmt.Printf("%s=%s\n", k, d.Env[k])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func envSetCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "set <app> KEY=VALUE...",
		Short: "Set one or more environment variables; the deployment is redeployed",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			changes, err := parsePairs(args[1:])
			if err != nil {
				return err
			}
			return changeEnv(cmd, deps, projectName, args[0], func(env map[string]string) error {
				for k, v := range changes {
					env[k] = v
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func envUnsetCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "unset <app> KEY...",
		Short: "Remove one or more environment variables; the deployment is redeployed",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return changeEnv(cmd, deps, projectName, args[0], func(env map[string]string) error {
				for _, k := range args[1:] {
					if _, ok := env[k]; !ok {
						return fmt.Errorf("%s has no variable %q", args[0], k)
					}
					delete(env, k)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

// changeEnv reads the environment as it was set, lets change edit it and sends
// the whole of it back: the API replaces the environment, it does not merge.
func changeEnv(cmd *cobra.Command, deps *Deps, projectName, app string, change func(map[string]string) error) error {
	ctx := cmd.Context()
	project, err := resolveProject(ctx, deps, projectName)
	if err != nil {
		return err
	}
	d, err := deps.Client.GetDeployment(ctx, project.ID, app)
	if err != nil {
		return fmt.Errorf("get deployment: %w", err)
	}
	env := make(map[string]string, len(d.Env))
	for k, v := range d.Env {
		// Writing a mask back would turn every one of these into the literal
		// text ***. Better to refuse than to break an app quietly.
		if v == maskedValue {
			return fmt.Errorf("%s was deployed before its variables were kept as set, so their values cannot be read back; "+
				"redeploy it once with heroctl deploy and try again", app)
		}
		env[k] = v
	}
	if err := change(env); err != nil {
		return err
	}
	if err := validateEnv(env); err != nil {
		return err
	}
	if err := deps.Client.SetDeploymentEnv(ctx, project.ID, app, env); err != nil {
		return fmt.Errorf("set environment: %w", err)
	}
	fmt.Printf("Environment of %q updated (%d variable(s)); it is being redeployed.\n", app, len(env))
	return nil
}

// parsePairs reads KEY=VALUE arguments. The value may itself contain = and may be empty.
func parsePairs(args []string) (map[string]string, error) {
	out := make(map[string]string, len(args))
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("%q is not KEY=VALUE", a)
		}
		out[k] = v
	}
	return out, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
