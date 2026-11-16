package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
	"github.com/Infra-Heroes/heroctl/internal/managed"
)

func dbCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Managed databases declared with [[databases]] in hero.toml",
		Long: `A database is declared next to the app in hero.toml and created on the next
deploy:

  [[databases]]
  name    = "db"
  engine  = "postgres"   # postgres, mysql or redis
  version = "17"         # optional
  size_gb = 5            # optional

heroctl then runs it as a private deployment of its own in the project, with a
volume and a generated password, and gives the app its connection URL as
DATABASE_URL (REDIS_URL for redis). These commands list and remove them.`,
	}
	cmd.AddCommand(dbListCmd(deps), dbDestroyCmd(deps))
	return cmd
}

func managedDatabases(deps []client.Deployment) []client.Deployment {
	var out []client.Deployment
	for _, d := range deps {
		if d.Labels[managed.LabelEngine] != "" {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AppName < out[j].AppName })
	return out
}

func dbListCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the managed databases of a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			all, err := deps.Client.ListDeployments(ctx, project.ID)
			if err != nil {
				return fmt.Errorf("list deployments: %w", err)
			}
			dbs := managedDatabases(all)
			if len(dbs) == 0 {
				fmt.Println("No managed databases. Declare one with [[databases]] in hero.toml.")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "DATABASE\tENGINE\tFOR APP\tSTATUS\tIMAGE")
			for _, d := range dbs {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", d.AppName, d.Labels[managed.LabelEngine], dash(d.Labels[managed.LabelFor]), d.Status, shortImage(d.Image))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func dbDestroyCmd(deps *Deps) *cobra.Command {
	var projectName string
	var force bool
	cmd := &cobra.Command{
		Use:   "destroy <database>",
		Short: "Delete a managed database with its data, volume and secrets",
		Long: `Deletes the database deployment, its volume (all data) and its two secrets.
This cannot be undone. <database> is the name shown by "heroctl db list".`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			all, err := deps.Client.ListDeployments(ctx, project.ID)
			if err != nil {
				return fmt.Errorf("list deployments: %w", err)
			}
			var target *client.Deployment
			for _, d := range managedDatabases(all) {
				if d.AppName == args[0] {
					d := d
					target = &d
				}
			}
			if target == nil {
				return fmt.Errorf("%q is not a managed database of project %q (see: heroctl db list --project %s)", args[0], projectName, projectName)
			}
			if !force {
				fmt.Printf("Permanently delete database %q with all its data? [y/N] ", target.AppName)
				sc := bufio.NewScanner(os.Stdin)
				sc.Scan()
				if a := strings.ToLower(strings.TrimSpace(sc.Text())); a != "y" && a != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}
			if err := destroyDatabase(ctx, deps, project.ID, *target); err != nil {
				return err
			}
			fmt.Printf("Database %s deleted.\n", target.AppName)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

// destroyDatabase deletes a managed database's deployment, its volume and its two
// secrets. The volume and the secrets are cleaned up on a best-effort basis: once
// the deployment is gone the database is gone, and a leftover is only clutter.
func destroyDatabase(ctx context.Context, deps *Deps, projectID string, target client.Deployment) error {
	forApp := target.Labels[managed.LabelFor]
	n := managed.NamesFor(forApp, strings.TrimPrefix(target.AppName, forApp+"-"))
	if err := deps.Client.DeleteDeployment(ctx, projectID, target.AppName); err != nil {
		return fmt.Errorf("delete deployment: %w", err)
	}
	if vols, err := deps.Client.ListVolumes(ctx, projectID); err == nil {
		for _, v := range vols {
			if v.Name == n.Volume {
				if err := deps.Client.DeleteVolume(ctx, projectID, v.ID); err != nil {
					fmt.Printf("Warning: the volume %s was not deleted: %v\n", v.Name, err)
				}
			}
		}
	}
	for _, k := range []string{n.PasswordKey, n.URLKey} {
		_ = deps.Client.DeleteSecret(ctx, projectID, k)
	}
	return nil
}
