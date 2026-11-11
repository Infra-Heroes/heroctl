package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// findVolume looks a volume up by name in a project.
func findVolume(ctx context.Context, deps *Deps, projectName, name string) (*client.Project, *client.Volume, error) {
	project, err := resolveProject(ctx, deps, projectName)
	if err != nil {
		return nil, nil, err
	}
	vols, err := deps.Client.ListVolumes(ctx, project.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list volumes: %w", err)
	}
	for i := range vols {
		if vols[i].Name == name {
			return project, &vols[i], nil
		}
	}
	return nil, nil, fmt.Errorf("volume %q not found in project %q", name, projectName)
}

// confirm asks a yes/no question. Anything but y or yes is a no.
func confirm(in io.Reader, prompt string) bool {
	fmt.Print(prompt + " [y/N] ")
	scanner := bufio.NewScanner(in)
	scanner.Scan()
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func humanSize(b int64) string {
	if b <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f GB", float64(b)/1024/1024/1024)
}

func volumesSnapshotsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshots",
		Short: "Recovery points of a volume: take, list, restore, remove, schedule",
		Long: `Recovery points (snapshots) of a volume.

Taking one does not stop anything; it is crash-consistent. Restoring one
discards everything written since and needs the deployment stopped first.
Automatic snapshots are named auto-<UTC time>; only those are ever pruned by
the schedule's retention.`,
	}
	cmd.AddCommand(
		snapshotsListCmd(deps),
		snapshotsCreateCmd(deps),
		snapshotsDeleteCmd(deps),
		snapshotsRestoreCmd(deps),
		snapshotsScheduleCmd(deps),
	)
	return cmd
}

func snapshotsListCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "list <volume>",
		Short: "List a volume's recovery points",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, vol, err := findVolume(ctx, deps, projectName, args[0])
			if err != nil {
				return err
			}
			snaps, err := deps.Client.ListSnapshots(ctx, project.ID, vol.ID)
			if err != nil {
				return fmt.Errorf("list snapshots: %w", err)
			}
			if len(snaps) == 0 {
				fmt.Printf("No recovery points. Take one with: heroctl volumes snapshots create %s --project %s\n", vol.Name, projectName)
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSIZE\tKIND")
			for _, s := range snaps {
				kind := "manual"
				if strings.HasPrefix(s.Name, "auto-") {
					kind = "automatic"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, humanSize(s.SizeBytes), kind)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func snapshotsCreateCmd(deps *Deps) *cobra.Command {
	var projectName, name string
	cmd := &cobra.Command{
		Use:   "create <volume>",
		Short: "Take a recovery point (the volume keeps running)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, vol, err := findVolume(ctx, deps, projectName, args[0])
			if err != nil {
				return err
			}
			got, err := deps.Client.CreateSnapshot(ctx, project.ID, vol.ID, name)
			if err != nil {
				return fmt.Errorf("create snapshot: %w", err)
			}
			fmt.Printf("Recovery point %q taken for volume %q.\n", got, vol.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&name, "name", "", "Name for the recovery point (default: a sortable timestamp)")
	return cmd
}

func snapshotsDeleteCmd(deps *Deps) *cobra.Command {
	var (
		projectName string
		force       bool
	)
	cmd := &cobra.Command{
		Use:   "delete <volume> <snapshot>",
		Short: "Remove a recovery point",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, vol, err := findVolume(ctx, deps, projectName, args[0])
			if err != nil {
				return err
			}
			if !force && !confirm(os.Stdin, fmt.Sprintf("Remove recovery point %q of volume %q?", args[1], vol.Name)) {
				fmt.Println("Aborted.")
				return nil
			}
			if err := deps.Client.DeleteSnapshot(ctx, project.ID, vol.ID, args[1]); err != nil {
				return fmt.Errorf("delete snapshot: %w", err)
			}
			fmt.Printf("Recovery point %q removed.\n", args[1])
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

func snapshotsRestoreCmd(deps *Deps) *cobra.Command {
	var (
		projectName string
		force       bool
	)
	cmd := &cobra.Command{
		Use:   "restore <volume> <snapshot>",
		Short: "Roll a volume back to a recovery point (discards everything written since)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, vol, err := findVolume(ctx, deps, projectName, args[0])
			if err != nil {
				return err
			}
			// Refused here as well as by the server: failing before the
			// question is kinder than asking one that cannot be acted on.
			if vol.Status == "attached" {
				return fmt.Errorf("volume %q is attached to a deployment M-bM-^@M-^T stop the deployment first: rolling back under a mounted filesystem corrupts it", vol.Name)
			}
			if !force && !confirm(os.Stdin, fmt.Sprintf(
				"Roll volume %q back to %q? Everything written since is discarded and there is no undo.", vol.Name, args[1])) {
				fmt.Println("Aborted.")
				return nil
			}
			if err := deps.Client.RestoreSnapshot(ctx, project.ID, vol.ID, args[1]); err != nil {
				return fmt.Errorf("restore snapshot: %w", err)
			}
			fmt.Printf("Volume %q restored to %q.\n", vol.Name, args[1])
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

func snapshotsScheduleCmd(deps *Deps) *cobra.Command {
	var (
		projectName string
		hour, keep  int
		off, on     bool
	)
	cmd := &cobra.Command{
		Use:   "schedule <volume>",
		Short: "Show or set the automatic daily snapshot (--at, --keep, --off)",
		Long: `Without flags, shows the volume's automatic-snapshot schedule.

  heroctl volumes snapshots schedule data --project app --at 3 --keep 10
  heroctl volumes snapshots schedule data --project app --off

--at is the hour in UTC (0-23); --keep is how many automatic ones to keep (1-14).
Recovery points you took yourself are never removed by the schedule.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if off && (cmd.Flags().Changed("at") || cmd.Flags().Changed("keep") || on) {
				return fmt.Errorf("--off cannot be combined with --at, --keep or --on")
			}
			project, vol, err := findVolume(ctx, deps, projectName, args[0])
			if err != nil {
				return err
			}

			var pol *client.SnapshotPolicy
			if cmd.Flags().Changed("at") || cmd.Flags().Changed("keep") || off || on {
				req := client.SnapshotPolicyRequest{}
				enabled := !off
				req.Enabled = &enabled
				if cmd.Flags().Changed("at") {
					req.HourUTC = &hour
				}
				if cmd.Flags().Changed("keep") {
					req.Keep = &keep
				}
				pol, err = deps.Client.PutSnapshotPolicy(ctx, project.ID, vol.ID, req)
				if err != nil {
					return fmt.Errorf("save schedule: %w", err)
				}
			} else {
				pol, err = deps.Client.GetSnapshotPolicy(ctx, project.ID, vol.ID)
				if err != nil {
					return fmt.Errorf("read schedule: %w", err)
				}
			}
			printPolicy(vol.Name, pol)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().IntVar(&hour, "at", 2, "Hour of the day in UTC (0-23)")
	cmd.Flags().IntVar(&keep, "keep", 7, "How many automatic recovery points to keep (1-14)")
	cmd.Flags().BoolVar(&off, "off", false, "Turn automatic snapshots off (existing ones stay)")
	cmd.Flags().BoolVar(&on, "on", false, "Turn automatic snapshots on with the current or default settings")
	return cmd
}

func printPolicy(volume string, p *client.SnapshotPolicy) {
	if !p.Enabled {
		fmt.Printf("Volume %q: automatic snapshots are off.\n", volume)
		return
	}
	fmt.Printf("Volume %q: a recovery point every day at %02d:00 UTC, newest %d kept.\n", volume, p.HourUTC, p.Keep)
	if p.LastStatus != nil && *p.LastStatus == "error" {
		reason := "no reason given"
		if p.LastError != nil {
			reason = *p.LastError
		}
		fmt.Printf("The last automatic run failed: %s (retried shortly).\n", reason)
	} else if p.LastRunAt != nil {
		fmt.Printf("Last automatic run: %s\n", *p.LastRunAt)
	}
}
