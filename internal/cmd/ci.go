package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// heroctl ci github writes the workflow files that deploy a repository with the
// GitHub Action, so a customer does not have to learn the Action's inputs, the
// pull_request event types or the permissions a preview needs.

var (
	ciProjectRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	ciBranchRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	ciImageRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]*$`)
)

const deployWorkflow = `name: Deploy to Infra-Heroes
on:
  push:
    branches: [@BRANCH@]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: infra-heroes/deploy-action@v1
        with:
          token: ${{ secrets.HERO_TOKEN }}
          project: @PROJECT@
@IMAGE@`

const previewWorkflow = `name: Preview
on:
  pull_request:
    types: [opened, synchronize, reopened, closed]
permissions:
  contents: read
  pull-requests: write
jobs:
  preview:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        if: github.event.action != 'closed'
      - uses: infra-heroes/deploy-action@v1
        with:
          token: ${{ secrets.HERO_TOKEN }}
          project: @PROJECT@
@IMAGE@          preview: true
`

// ciWorkflow fills a template. The values were checked against the patterns above,
// so none of them can carry a newline, a quote or a YAML construct.
func ciWorkflow(tmpl, project, branch, image string) string {
	imageLine := ""
	if image != "" {
		imageLine = "          image: " + image + "\n"
	}
	return strings.NewReplacer("@PROJECT@", project, "@BRANCH@", branch, "@IMAGE@", imageLine).Replace(tmpl)
}

// setGitHubSecret stores value as the repository secret name through the GitHub CLI.
// The value goes in on stdin, never on the command line. A variable so tests do not
// need gh.
var setGitHubSecret = func(ctx context.Context, name, value string) error {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return errors.New("the GitHub CLI (gh) is not installed or not on PATH")
	}
	c := exec.CommandContext(ctx, gh, "secret", "set", name) // #nosec G204 -- fixed program and arguments; the secret value is passed on stdin
	c.Stdin = strings.NewReader(value)
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("gh secret set %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ciCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Set up deploying from CI",
	}
	cmd.AddCommand(ciGitHubCmd(deps))
	return cmd
}

func ciGitHubCmd(deps *Deps) *cobra.Command {
	var project, branch, image string
	var preview, force, setSecret bool

	cmd := &cobra.Command{
		Use:   "github",
		Short: "Write the GitHub workflow that deploys this repository",
		Long: `Writes .github/workflows/deploy.yml, which deploys the app of this directory with the
Infra-Heroes GitHub Action on every push to a branch, and with --preview also
.github/workflows/preview.yml, which gives every pull request its own preview
and removes it when the pull request closes.

The workflows read a deploy token from the repository secret HERO_TOKEN:

  heroctl ci github --project my-project --set-secret    create the token and set the secret (needs gh)
  heroctl ci github --project my-project                 only the files; it tells you the two commands

  heroctl ci github --project my-project --preview       also previews of pull requests
  heroctl ci github --project my-project --image nginx:1.31   deploy an existing image, no Docker build`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if !ciProjectRe.MatchString(project) {
				return fmt.Errorf("--project %q is not a project name", project)
			}
			if !ciBranchRe.MatchString(branch) {
				return fmt.Errorf("--branch %q is not a branch name", branch)
			}
			if image != "" && !ciImageRe.MatchString(image) {
				return fmt.Errorf("--image %q is not an image reference", image)
			}
			if _, err := os.Stat("hero.toml"); err != nil {
				return errors.New("hero.toml not found in this directory: run this in the root of your app (see: heroctl validate)")
			}

			files := map[string]string{
				filepath.Join(".github", "workflows", "deploy.yml"): ciWorkflow(deployWorkflow, project, branch, image),
			}
			if preview {
				files[filepath.Join(".github", "workflows", "preview.yml")] = ciWorkflow(previewWorkflow, project, branch, image)
			}
			// Check every file before writing any: half a setup is worse than none.
			if !force {
				for name := range files {
					if _, err := os.Stat(name); err == nil {
						return fmt.Errorf("%s exists; use --force to overwrite it", name)
					}
				}
			}
			if err := os.MkdirAll(filepath.Join(".github", "workflows"), 0o750); err != nil {
				return err
			}
			for _, name := range []string{"deploy.yml", "preview.yml"} {
				path := filepath.Join(".github", "workflows", name)
				content, ok := files[path]
				if !ok {
					continue
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", path)
			}

			if setSecret {
				name := "github-" + tokenNameSafe(filepath.Base(mustAbs(".")))
				pat, err := deps.Client.CreatePAT(ctx, name, "deploy")
				if err != nil {
					return fmt.Errorf("create token: %w", err)
				}
				if err := setGitHubSecret(ctx, "HERO_TOKEN", pat.Token); err != nil {
					return fmt.Errorf("the files are written, but the secret was not set: %w\n"+
						"The token %q (%s) was created; set it yourself or delete it with: heroctl tokens delete %s",
						err, pat.Name, pat.ID, pat.ID)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created the deploy token %q and set the repository secret HERO_TOKEN.\n", pat.Name)
			} else {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\nNext: give the workflow a deploy token as the repository secret HERO_TOKEN:")
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  heroctl tokens create --name github --scope deploy")
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  gh secret set HERO_TOKEN        (or: repository Settings, Secrets and variables, Actions)")
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Commit and push: the workflow deploys to project %s on every push to %s.\n", project, branch)
			if preview {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Pull requests get a preview; a preview with [[databases]] needs a token with scope all to be cleaned up completely.")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "Project to deploy to (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&branch, "branch", "main", "Branch whose pushes deploy")
	cmd.Flags().StringVar(&image, "image", "", "Deploy this existing image instead of building one")
	cmd.Flags().BoolVar(&preview, "preview", false, "Also write the workflow that previews pull requests")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing workflow files")
	cmd.Flags().BoolVar(&setSecret, "set-secret", false, "Create a deploy token and set it as the repository secret HERO_TOKEN with gh")
	return cmd
}

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

// tokenNameSafe keeps a directory name usable as part of a token name.
func tokenNameSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		}
		return '-'
	}, s)
}
