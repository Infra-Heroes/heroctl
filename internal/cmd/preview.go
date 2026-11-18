package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/managed"
	"github.com/Infra-Heroes/heroctl/internal/toml"
)

// A preview is the same app deployed under another name for as long as a pull
// request is open: <app>-<preview>, for example my-app-pr-12. It shares nothing
// with the real app that could be damaged: not its domains, not its volumes, and
// the databases it needs are its own, empty ones.

// labelPreview marks a deployment as the preview of a pull request.
const labelPreview = "hero-preview"

// labelExpiresAt is when hero-api stops a preview, in Unix seconds.
const labelExpiresAt = "hero-expires-at"

// maxPreviewTTL keeps a typo from making a preview permanent in all but name.
const maxPreviewTTL = 90 * 24 * time.Hour

// previewExpiry turns "72h", "90m" or "7d" into the moment the preview stops.
func previewExpiry(ttl string, now time.Time) (time.Time, error) {
	var d time.Duration
	var err error
	if strings.HasSuffix(ttl, "d") {
		var days int
		days, err = strconv.Atoi(strings.TrimSuffix(ttl, "d"))
		d = time.Duration(days) * 24 * time.Hour
	} else {
		d, err = time.ParseDuration(ttl)
	}
	if err != nil || d <= 0 {
		return time.Time{}, fmt.Errorf("--ttl %q is not a duration: use for example 90m, 72h or 7d", ttl)
	}
	if d < 10*time.Minute || d > maxPreviewTTL {
		return time.Time{}, fmt.Errorf("--ttl must be between 10m and 90d")
	}
	return now.Add(d), nil
}

var previewNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

// previewAppName is the app name of the preview called name, or an error if the
// result would not be a valid app name.
func previewAppName(app, name string) (string, error) {
	if !previewNameRe.MatchString(name) {
		return "", fmt.Errorf("--preview %q is invalid: lowercase letters, digits and hyphens, up to 32 characters (for example pr-12)", name)
	}
	full := app + "-" + name
	if err := validateAppName(full); err != nil {
		return "", fmt.Errorf("--preview %q makes the app name too long: %w", name, err)
	}
	return full, nil
}

// applyPreview turns the parsed hero.toml of the app into the one of its preview
// and returns what it had to leave out, as messages for the user.
func applyPreview(cfg *toml.HeroConfig, name string) ([]string, error) {
	full, err := previewAppName(cfg.App.Name, name)
	if err != nil {
		return nil, err
	}
	var notes []string
	cfg.App.Name = full
	// The platform lets a deploy-scoped token delete only deployments carrying this
	// label, which is what lets a CI job remove the preview of a closed pull request.
	if cfg.Labels == nil {
		cfg.Labels = map[string]string{}
	}
	cfg.Labels[labelPreview] = "true"
	if len(cfg.App.CustomDomains) > 0 {
		notes = append(notes, "custom domains are not used by a preview, it gets its own address")
		cfg.App.CustomDomains = nil
		delete(cfg.Labels, "hero-custom-domains")
	}
	if len(cfg.Volumes) > 0 {
		notes = append(notes, "volumes are not attached to a preview: a volume belongs to one deployment, and the real app keeps it")
		cfg.Volumes = nil
	}
	if len(cfg.Jobs) > 0 {
		notes = append(notes, "scheduled jobs are not run by a preview: they would act on a schedule for as long as the pull request is open")
		cfg.Jobs = nil
	}
	if len(cfg.Databases) > 0 {
		notes = append(notes, "the preview gets its own, empty database(s); delete them with: heroctl preview delete "+name)
	}
	// A preview that nobody opens should not run: it starts on the first request.
	// A worker has no request to start it, so it runs for as long as the preview.
	if !cfg.Deploy.Worker {
		cfg.Deploy.ScaleToZero = true
	}
	return notes, nil
}

func previewCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Manage preview deployments of a pull request",
		Long: `A preview is your app deployed under another name, <app>-<preview>, while a pull
request is open. Create one with:

  heroctl deploy --project p --preview pr-12

and remove it, with the databases it created, when the pull request is closed:

  heroctl preview delete pr-12 --project p`,
	}
	cmd.AddCommand(previewDeleteCmd(deps))
	return cmd
}

func previewDeleteCmd(deps *Deps) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "delete <preview>",
		Short: "Delete a preview deployment and the databases it created",
		Long: `Deletes <app>-<preview> (the app name is read from hero.toml in this directory)
and every managed database that was created for it, with its data. A preview that
does not exist is not an error, so this is safe to run when a pull request is closed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			f, err := os.Open("hero.toml")
			if err != nil {
				return fmt.Errorf("hero.toml not found in current directory: %w", err)
			}
			cfg, err := toml.Parse(f)
			_ = f.Close()
			if err != nil {
				return err
			}
			full, err := previewAppName(cfg.App.Name, args[0])
			if err != nil {
				return err
			}
			// A preview name must never reach the real app: <app>-<preview> is always
			// longer than <app>, but say it out loud for the next reader.
			if full == cfg.App.Name {
				return fmt.Errorf("refusing to delete %q", full)
			}
			project, err := resolveProject(ctx, deps, projectName)
			if err != nil {
				return err
			}
			all, err := deps.Client.ListDeployments(ctx, project.ID)
			if err != nil {
				return fmt.Errorf("list deployments: %w", err)
			}
			found := false
			for _, d := range all {
				if d.AppName == full {
					found = true
				}
			}
			// The app first: it is the part a deploy-scoped token may delete, and the
			// part that runs and costs. The databases it created come after.
			if found {
				if err := deps.Client.DeleteDeployment(ctx, project.ID, full); err != nil {
					return fmt.Errorf("delete deployment: %w", err)
				}
				fmt.Printf("Preview %s deleted.\n", full)
			} else {
				fmt.Printf("Preview %s does not exist, nothing to delete.\n", full)
			}
			var left []string
			for _, d := range managedDatabases(all) {
				if d.Labels[managed.LabelFor] != full {
					continue
				}
				if err := destroyDatabase(ctx, deps, project.ID, d); err != nil {
					fmt.Fprintf(os.Stderr, "Database %s was not deleted: %v\n", d.AppName, err)
					left = append(left, d.AppName)
					continue
				}
				fmt.Printf("Database %s deleted.\n", d.AppName)
			}
			if len(left) > 0 {
				return fmt.Errorf("the preview's database(s) %v are still there: deleting a database needs a token with scope all (a deploy token may delete the preview app only); run: heroctl db destroy <name> --project %s", left, projectName)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
