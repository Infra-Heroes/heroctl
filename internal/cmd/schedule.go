package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func deploymentsScheduleCmd(deps *Deps) *cobra.Command {
	var (
		projectName, stop, start, timezone string
		on, off, remove                    bool
	)
	cmd := &cobra.Command{
		Use:   "schedule <app>",
		Short: "Show or set when a deployment is stopped and started",
		Long: `A timetable stops a deployment at one time and starts it at another, for
example to shut a staging app down overnight and at weekends. Times are cron
expressions (minute hour day-of-month month day-of-week) in the given timezone.

Without flags the timetable is shown.

  heroctl deployments schedule my-app --project p --stop "0 20 * * 1-5" --start "0 7 * * 1-5" --timezone Europe/Berlin
  heroctl deployments schedule my-app --project p --stop "0 22 * * *"      change one side, keep the other
  heroctl deployments schedule my-app --project p --off                     pause it, keep the times
  heroctl deployments schedule my-app --project p --on                      resume it
  heroctl deployments schedule my-app --project p --remove                  delete it

Only a deployment that scales to zero is woken by traffic; a scheduled stop holds
until the schedule (or you) starts it again.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			app := args[0]
			changing := cmd.Flags().Changed("stop") || cmd.Flags().Changed("start") ||
				cmd.Flags().Changed("timezone") || on || off
			if remove && (changing) {
				return fmt.Errorf("--remove cannot be combined with other changes")
			}
			if on && off {
				return fmt.Errorf("--on and --off contradict each other")
			}

			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}

			switch {
			case remove:
				if err := deps.Client.DeleteDeploymentSchedule(ctx, project.ID, app); err != nil {
					return fmt.Errorf("remove schedule: %w", err)
				}
				fmt.Printf("Schedule of %q removed.\n", app)
				return nil

			case changing:
				// The API replaces the whole timetable, so start from what is there.
				cur, err := deps.Client.GetDeploymentSchedule(ctx, project.ID, app)
				if err != nil {
					return fmt.Errorf("read schedule: %w", err)
				}
				req := client.DeploymentScheduleRequest{
					StartCron: cur.StartCron, StopCron: cur.StopCron, Timezone: cur.Timezone,
				}
				if cmd.Flags().Changed("stop") {
					req.StopCron = stop
				}
				if cmd.Flags().Changed("start") {
					req.StartCron = start
				}
				if cmd.Flags().Changed("timezone") {
					req.Timezone = timezone
				}
				enabled := cur.Enabled
				switch {
				case off:
					enabled = false
				case on:
					enabled = true
				case cmd.Flags().Changed("stop") || cmd.Flags().Changed("start"):
					// Setting times means wanting them to run.
					enabled = true
				}
				req.Enabled = &enabled
				got, err := deps.Client.PutDeploymentSchedule(ctx, project.ID, app, req)
				if err != nil {
					return fmt.Errorf("save schedule: %w", err)
				}
				printSchedule(app, got)
				return nil

			default:
				got, err := deps.Client.GetDeploymentSchedule(ctx, project.ID, app)
				if err != nil {
					return fmt.Errorf("read schedule: %w", err)
				}
				printSchedule(app, got)
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&stop, "stop", "", `Cron expression for stopping, e.g. "0 20 * * 1-5"`)
	cmd.Flags().StringVar(&start, "start", "", `Cron expression for starting, e.g. "0 7 * * 1-5"`)
	cmd.Flags().StringVar(&timezone, "timezone", "", "Time zone of the expressions, e.g. Europe/Berlin (default UTC)")
	cmd.Flags().BoolVar(&on, "on", false, "Resume a paused schedule")
	cmd.Flags().BoolVar(&off, "off", false, "Pause the schedule, keeping its times")
	cmd.Flags().BoolVar(&remove, "remove", false, "Delete the schedule")
	return cmd
}

func printSchedule(app string, s *client.DeploymentSchedule) {
	if s.StopCron == "" && s.StartCron == "" {
		fmt.Printf("%s has no schedule. Set one with: heroctl deployments schedule %s --project <project> --stop \"0 20 * * *\" --start \"0 7 * * *\"\n", app, app)
		return
	}
	state := "active"
	if !s.Enabled {
		state = "paused"
	}
	tz := s.Timezone
	if tz == "" {
		tz = "UTC"
	}
	fmt.Printf("Schedule of %s (%s, %s):\n", app, state, tz)
	if s.StopCron != "" {
		fmt.Printf("  stop   %s", s.StopCron)
		if s.Enabled && s.NextStopAt != nil {
			fmt.Printf("   next: %s", *s.NextStopAt)
		}
		fmt.Println()
	}
	if s.StartCron != "" {
		fmt.Printf("  start  %s", s.StartCron)
		if s.Enabled && s.NextStartAt != nil {
			fmt.Printf("   next: %s", *s.NextStartAt)
		}
		fmt.Println()
	}
}
