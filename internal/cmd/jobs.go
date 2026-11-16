package cmd

import (
	"context"
	"fmt"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
	"github.com/Infra-Heroes/heroctl/internal/toml"
)

// Labels that mark a deployment as a scheduled job. hero-api reads the three
// hero-job-schedule/-timezone/-timeout ones; the other two are for heroctl, to find
// the jobs of an app again.
const (
	labelJobFor      = "hero-job-for"
	labelJobName     = "hero-job-name"
	labelJobSchedule = "hero-job-schedule"
	labelJobTimezone = "hero-job-timezone"
	labelJobTimeout  = "hero-job-timeout"
	jobCommandEnv    = "HERO_COMMAND"
)

// jobRequest is the deployment that runs one [[jobs]] entry: the app's image with
// another command, on a schedule, with no port and no address.
func jobRequest(cfg *toml.HeroConfig, j toml.JobConfig, image string) client.CreateDeploymentRequest {
	env := make(map[string]string, len(cfg.Env)+1)
	for k, v := range cfg.Env {
		env[k] = v
	}
	env[jobCommandEnv] = j.Command

	labels := map[string]string{
		labelJobFor:      cfg.App.Name,
		labelJobName:     j.Name,
		labelJobSchedule: j.Schedule,
	}
	if j.Timezone != "" {
		labels[labelJobTimezone] = j.Timezone
	}
	if j.Timeout != "" {
		labels[labelJobTimeout] = j.Timeout
	}
	cpu, mem := j.CPU, j.MemoryMB
	if cpu == 0 {
		cpu = cfg.Deploy.CPU
	}
	if mem == 0 {
		mem = cfg.Deploy.MemoryMB
	}
	return client.CreateDeploymentRequest{
		AppName:         j.AppName(cfg.App.Name),
		Image:           image,
		CPU:             cpu,
		MemoryMB:        mem,
		Port:            cfg.Deploy.Port,
		Env:             env,
		Labels:          labels,
		HealthPath:      "/",
		ServiceScope:    "internal",
		HealthCheckType: "none",
		MinReplicas:     1,
		MaxReplicas:     1,
	}
}

// deployJobs creates or updates the deployment of every [[jobs]] entry and removes
// the ones that used to be in hero.toml and no longer are. It runs after the app
// itself is up: a job of an app that did not start is not worth scheduling.
func deployJobs(ctx context.Context, deps *Deps, project *client.Project, cfg *toml.HeroConfig, image string) error {
	if len(cfg.Jobs) == 0 && !hasJobsFor(ctx, deps, project.ID, cfg.App.Name) {
		return nil
	}
	want := map[string]bool{}
	for _, j := range cfg.Jobs {
		want[j.AppName(cfg.App.Name)] = true
		if _, err := deps.Client.CreateDeployment(ctx, project.ID, jobRequest(cfg, j, image)); err != nil {
			return fmt.Errorf("job %q: %w", j.Name, err)
		}
		fmt.Printf("Job %s scheduled: %s", j.Name, j.Schedule)
		if j.Timezone != "" {
			fmt.Printf(" (%s)", j.Timezone)
		}
		fmt.Println()
	}
	all, err := deps.Client.ListDeployments(ctx, project.ID)
	if err != nil {
		return fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range jobsOf(all, cfg.App.Name) {
		if want[d.AppName] {
			continue
		}
		if err := deps.Client.DeleteDeployment(ctx, project.ID, d.AppName); err != nil {
			return fmt.Errorf("remove job %s: %w", d.Labels[labelJobName], err)
		}
		fmt.Printf("Job %s is not in hero.toml any more: removed.\n", d.Labels[labelJobName])
	}
	return nil
}

func hasJobsFor(ctx context.Context, deps *Deps, projectID, app string) bool {
	all, err := deps.Client.ListDeployments(ctx, projectID)
	return err == nil && len(jobsOf(all, app)) > 0
}

// newestPerApp keeps one deployment per app name, the newest. Every deploy writes a
// new revision, and the list holds all of them; a job should show, and be removed,
// once.
func newestPerApp(all []client.Deployment) []client.Deployment {
	newest := map[string]client.Deployment{}
	for _, d := range all {
		if cur, ok := newest[d.AppName]; !ok || d.CreatedAt > cur.CreatedAt {
			newest[d.AppName] = d
		}
	}
	out := make([]client.Deployment, 0, len(newest))
	for _, d := range newest {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AppName < out[j].AppName })
	return out
}

// jobsOf are the scheduled jobs of app among deployments.
func jobsOf(all []client.Deployment, app string) []client.Deployment {
	var out []client.Deployment
	for _, d := range newestPerApp(all) {
		if d.Labels[labelJobFor] == app && d.Labels[labelJobSchedule] != "" {
			out = append(out, d)
		}
	}
	return out
}

// scheduledJobs are the scheduled jobs of every app in the list.
func scheduledJobs(all []client.Deployment) []client.Deployment {
	var out []client.Deployment
	for _, d := range newestPerApp(all) {
		if d.Labels[labelJobSchedule] != "" {
			out = append(out, d)
		}
	}
	return out
}

func jobsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "List scheduled jobs, run one now, see how the runs went",
		Long: `A job is a [[jobs]] entry of hero.toml: your app's image run with another
command on a cron schedule. It costs nothing between runs and is billed for the
time a run takes.

  heroctl jobs list --project p
  heroctl jobs run my-app-cleanup --project p        start a run now
  heroctl jobs runs my-app-cleanup --project p       the newest runs, with exit code
  heroctl logs my-app-cleanup --project p --since 2h what a run printed`,
	}
	cmd.AddCommand(jobsListCmd(deps), jobsRunCmd(deps), jobsRunsCmd(deps))
	return cmd
}

func jobsListCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the scheduled jobs of a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, err := resolveProject(cmd.Context(), deps, projectName)
			if err != nil {
				return err
			}
			all, err := deps.Client.ListDeployments(cmd.Context(), project.ID)
			if err != nil {
				return fmt.Errorf("list deployments: %w", err)
			}
			jobs := scheduledJobs(all)
			if len(jobs) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No scheduled jobs. Add a [[jobs]] entry to hero.toml and deploy.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "JOB\tSCHEDULE\tTIMEZONE\tTIMEOUT\tSTATUS")
			for _, d := range jobs {
				tz, timeout := d.Labels[labelJobTimezone], d.Labels[labelJobTimeout]
				if tz == "" {
					tz = "UTC"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", d.AppName, d.Labels[labelJobSchedule], tz, timeout, d.Status)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func jobsRunCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "run <job>",
		Short: "Start a run of a job now",
		Long:  "Starts a run right away, outside the schedule. <job> is the name shown by \"heroctl jobs list\". A run that is still going is not started a second time.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, projectName)
			if err != nil {
				return err
			}
			if err := deps.Client.RunJob(cmd.Context(), project.ID, args[0]); err != nil {
				return fmt.Errorf("run job: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Run of %s started. Follow it with: heroctl logs %s --project %s --since 5m\n", args[0], args[0], projectName)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func jobsRunsCmd(deps *Deps) *cobra.Command {
	var projectName string
	var limit int
	cmd := &cobra.Command{
		Use:   "runs <job>",
		Short: "Show the newest runs of a job",
		Long:  "Shows when each run started, how long it took and the exit code of its command (0 is success). The exit code is read from the run's log and is missing while a run is going or once its log is older than 30 days.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := resolveProject(cmd.Context(), deps, projectName)
			if err != nil {
				return err
			}
			runs, err := deps.Client.ListJobRuns(cmd.Context(), project.ID, args[0], limit)
			if err != nil {
				return fmt.Errorf("list runs: %w", err)
			}
			if len(runs) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No runs yet.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "STARTED\tDURATION\tEXIT")
			for _, r := range runs {
				dur, exit := "running", "-"
				if r.Seconds != nil {
					dur = (time.Duration(*r.Seconds) * time.Second).String()
				}
				if r.ExitCode != nil {
					exit = fmt.Sprint(*r.ExitCode)
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Started.Local().Format("2006-01-02 15:04:05"), dur, exit)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().IntVar(&limit, "limit", 20, "How many runs to show (1 to 100)")
	return cmd
}
