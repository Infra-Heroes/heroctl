package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// minRevisionSuffix is how much of a revision id has to be typed. The list shows
// eight characters; anything shorter than four is more likely a typo than a name.
//
// It is the END of the id that names a revision: ids are time-ordered (UUIDv7),
// so everything deployed in the same minute starts with the same characters, and
// only the end tells them apart.
const minRevisionSuffix = 4

func deploymentsRevisionsCmd(deps *Deps) *cobra.Command {
	var projectName string
	var limit int
	cmd := &cobra.Command{
		Use:   "revisions <app>",
		Short: "List what has been deployed: the history a rollback goes back to",
		Long: `Every deploy, resize, environment change and rollback leaves a revision behind.
The newest is on top; the one that runs now is marked with *.

  heroctl deployments revisions my-app --project my-project
  heroctl deployments rollback my-app --project my-project              go back one revision
  heroctl deployments rollback my-app <revision> --project my-project   go back to a given one`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			got, err := deps.Client.ListDeploymentRevisions(ctx, project.ID, args[0], limit)
			if err != nil {
				return fmt.Errorf("list revisions: %w", err)
			}
			if len(got.Revisions) == 0 {
				fmt.Printf("%s has no revisions yet.\n", args[0])
				return nil
			}
			printRevisions(got)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().IntVar(&limit, "limit", 20, "How many revisions to show (newest first)")
	return cmd
}

func deploymentsRollbackCmd(deps *Deps) *cobra.Command {
	var projectName string
	var force bool
	cmd := &cobra.Command{
		Use:   "rollback <app> [revision]",
		Short: "Put an earlier revision of a deployment back into service",
		Long: `Redeploys an earlier image with the CPU, memory, environment and replica count it
had. Without a revision the one before the newest is used, which undoes the last
deploy or change. A revision is the id from "heroctl deployments revisions"; the
last characters are enough as long as they are unique.

The rollback is a deploy of its own and shows up in the history, so it can be
undone the same way. A deployment with a volume is stopped and started again,
which takes it offline for a short while.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			app := args[0]
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			// A page this size reaches far enough back to name a revision by its
			// first characters without asking the server to search.
			got, err := deps.Client.ListDeploymentRevisions(ctx, project.ID, app, 100)
			if err != nil {
				return fmt.Errorf("list revisions: %w", err)
			}
			want := ""
			if len(args) == 2 {
				want = args[1]
			}
			rev, err := pickRevision(got.Revisions, want)
			if err != nil {
				return err
			}

			if !force && !confirm(cmd.InOrStdin(), fmt.Sprintf(
				"Roll %q back to %s (%s, %d CPU, %d MB, %d replica(s))? It is redeployed.",
				app, shortID(rev.ID), shortImage(rev.Image), rev.CPU, rev.MemoryMB, rev.Replicas)) {
				fmt.Println("Aborted.")
				return nil
			}

			res, err := deps.Client.RollbackDeployment(ctx, project.ID, app, rev.ID)
			if err != nil {
				return fmt.Errorf("rollback: %w", err)
			}
			fmt.Printf("Rolling %q back to %s (%s). Check it with: heroctl deployments get %s --project %s\n",
				app, shortID(rev.ID), shortImage(res.Image), app, projectName)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&force, "force", false, "Do not ask for confirmation")
	return cmd
}

// pickRevision chooses the revision to roll back to from a history that comes
// newest first. No name means the one before the newest, so that "roll back"
// undoes the last change; a name is an id or the end of one.
func pickRevision(revs []client.DeploymentRevision, want string) (*client.DeploymentRevision, error) {
	if want == "" {
		if len(revs) < 2 {
			return nil, fmt.Errorf("there is no earlier revision to roll back to")
		}
		return &revs[1], nil
	}
	want = strings.ToLower(want)
	if len(want) < minRevisionSuffix {
		return nil, fmt.Errorf("revision %q is too short: give at least %d characters from the end of its id", want, minRevisionSuffix)
	}
	var found []*client.DeploymentRevision
	for i := range revs {
		if revs[i].ID == want || strings.HasSuffix(revs[i].ID, want) {
			found = append(found, &revs[i])
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("no revision ends with %q; list them with: heroctl deployments revisions <app> --project <project>", want)
	case 1:
		return found[0], nil
	default:
		return nil, fmt.Errorf("%d revisions end with %q; give more of the id", len(found), want)
	}
}

func printRevisions(r *client.DeploymentRevisions) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "\tREVISION\tIMAGE\tCPU\tMEM(MB)\tREPLICAS\tBY\tWHEN")
	for _, v := range r.Revisions {
		mark := " "
		if v.Current {
			mark = "*"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			mark, shortID(v.ID), shortImage(v.Image), v.CPU, v.MemoryMB, v.Replicas, dash(v.CreatedBy), when(v.CreatedAt))
	}
	_ = tw.Flush()
	if int(r.Total) > len(r.Revisions) {
		fmt.Printf("\nShowing %d of %d. More with --limit.\n", len(r.Revisions), r.Total)
	}
}

// shortID is the last eight characters: the part of a time-ordered id that
// differs from one revision to the next.
func shortID(id string) string {
	if len(id) > 8 {
		return id[len(id)-8:]
	}
	return id
}

// shortImage drops the registry and organisation, which are the same on every
// line, and keeps name:tag.
func shortImage(image string) string {
	if i := strings.LastIndex(image, "/"); i >= 0 {
		return image[i+1:]
	}
	return image
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func when(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("2006-01-02 15:04")
}
