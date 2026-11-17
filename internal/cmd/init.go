package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/toml"
)

// heroctl init writes a new repository's starting files: everything a deployable app
// needs next to its code, so a first deploy is `heroctl deploy` and nothing more.
// It never talks to the API and never overwrites a file that is already there.

var initNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}[a-z0-9]$|^[a-z0-9]$`)

// initTemplate is one language's source file, Dockerfile and ignore rules.
type initTemplate struct {
	port       int
	source     map[string]string // extra files, name -> content
	dockerfile string
	ignore     string
}

// The hello-world servers use only the language's standard library: nothing to install,
// nothing to pin, and a /healthz the platform's health check can use.
var initTemplates = map[string]initTemplate{
	"static": {
		port: 80,
		source: map[string]string{"index.html": `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>@NAME@</title></head>
<body><h1>@NAME@</h1><p>Deployed with heroctl.</p></body>
</html>
`,
			"healthz": "ok\n"},
		dockerfile: `FROM nginx:1.27-alpine
COPY index.html healthz /usr/share/nginx/html/
EXPOSE 80
`,
		ignore: ".git\nREADME.md\n",
	},
	"go": {
		port: 8080,
		source: map[string]string{
			"main.go": `package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "Hello from @NAME@") })

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
`,
			"go.mod": "module @NAME@\n\ngo 1.26\n",
		},
		dockerfile: `FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /app .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /app
EXPOSE 8080
ENTRYPOINT ["/app"]
`,
		ignore: ".git\nREADME.md\n",
	},
	"python": {
		port: 8080,
		source: map[string]string{"app.py": `import os
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"ok\n" if self.path == "/healthz" else b"Hello from @NAME@\n"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    HTTPServer(("", int(os.getenv("PORT", "8080"))), Handler).serve_forever()
`},
		dockerfile: `FROM python:3.13-slim
ENV PYTHONUNBUFFERED=1
WORKDIR /app
COPY app.py .
RUN useradd --system --uid 10001 app
USER app
EXPOSE 8080
CMD ["python", "app.py"]
`,
		ignore: ".git\n.venv\n__pycache__\nREADME.md\n",
	},
	"node": {
		port: 8080,
		source: map[string]string{"server.js": `const http = require("node:http");

http
  .createServer((req, res) => {
    res.setHeader("Content-Type", "text/plain; charset=utf-8");
    res.end(req.url === "/healthz" ? "ok\n" : "Hello from @NAME@\n");
  })
  .listen(Number(process.env.PORT || 8080));
`,
			"package.json": `{
  "name": "@NAME@",
  "version": "0.1.0",
  "private": true,
  "scripts": { "start": "node server.js" }
}
`},
		dockerfile: `FROM node:22-alpine
WORKDIR /app
COPY package.json server.js ./
USER node
EXPOSE 8080
CMD ["node", "server.js"]
`,
		ignore: ".git\nnode_modules\nREADME.md\n",
	},
}

const initHeroToml = `[app]
name = "@NAME@"

[deploy]
cpu = 1
memory_mb = 256
port = @PORT@
health_path = "/healthz"

[env]
LOG_LEVEL = "info"
`

const initReadme = `# @NAME@

Deployed on Infra-Heroes.

## Run it

` + "```" + `bash
heroctl login
heroctl deploy
` + "```" + `

` + "`hero.toml`" + ` describes the deployment: size, port, health check, environment and secrets.
Check it with ` + "`heroctl validate`" + `.

## Deploy from CI

` + "```" + `bash
heroctl ci github --project @NAME@ --preview --set-secret
` + "```" + `

(The workflows are already in ` + "`.github/workflows`" + `; this creates the deploy token and the
repository secret ` + "`HERO_TOKEN`" + `.)

## Develop

` + "```" + `bash
pre-commit install
` + "```" + `
`

const initPreCommit = `repos:
  - repo: https://github.com/pre-commit/pre-commit-hooks
    rev: v5.0.0
    hooks:
      - id: trailing-whitespace
      - id: end-of-file-fixer
      - id: check-yaml
      - id: check-toml
      - id: check-merge-conflict
      - id: detect-private-key
  - repo: https://github.com/hadolint/hadolint
    rev: v2.12.0
    hooks:
      - id: hadolint-docker
`

const initGitignore = `.env
.env.*
*.log
.DS_Store
.idea/
.vscode/
`

func initCmd() *cobra.Command {
	var name, lang string
	var force, noGit bool

	cmd := &cobra.Command{
		Use:   "init [directory]",
		Short: "Create a new app repository with everything it needs",
		Long: `Writes the starting files of a deployable app into a directory (default: the current one):

  hero.toml                      the deployment: size, port, health check, environment
  Dockerfile, .dockerignore      a small image for the chosen language
  a hello-world server           with /healthz, standard library only
  README.md                      how to deploy and develop
  .pre-commit-config.yaml        whitespace, YAML/TOML, private-key and Dockerfile checks
  .github/workflows/deploy.yml   deploy on push, and preview.yml for pull requests
  .gitignore

and runs git init unless it is already a repository. Existing files are never overwritten
unless you pass --force. Nothing is sent anywhere.

  heroctl init my-app --lang go
  heroctl init --lang python          in the current directory`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if name == "" {
				name = initSafeName(filepath.Base(abs))
			}
			if !initNameRe.MatchString(name) {
				return fmt.Errorf("--name %q must be lower case letters, digits and hyphens (1 to 40 characters)", name)
			}
			tmpl, ok := initTemplates[lang]
			if !ok {
				return fmt.Errorf("--lang %q is not one of: %s", lang, strings.Join(initLanguages(), ", "))
			}

			files := initFiles(name, tmpl)
			// The generated hero.toml has to be one the CLI itself accepts.
			if _, err := toml.Parse(strings.NewReader(files["hero.toml"])); err != nil {
				return fmt.Errorf("internal error: the generated hero.toml is invalid: %w", err)
			}

			if !force {
				var clashes []string
				for rel := range files {
					if _, err := os.Stat(filepath.Join(abs, rel)); err == nil {
						clashes = append(clashes, rel)
					}
				}
				if len(clashes) > 0 {
					sort.Strings(clashes)
					return fmt.Errorf("these files exist already: %s (use --force to overwrite them)", strings.Join(clashes, ", "))
				}
			}

			names := make([]string, 0, len(files))
			for rel := range files {
				names = append(names, rel)
			}
			sort.Strings(names)
			for _, rel := range names {
				path := filepath.Join(abs, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					return err
				}
				if err := os.WriteFile(path, []byte(files[rel]), 0o600); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", rel)
			}

			if !noGit {
				if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
					if git, lookErr := exec.LookPath("git"); lookErr == nil {
						c := exec.CommandContext(cmd.Context(), git, "init", "-q") // #nosec G204 -- fixed program and arguments
						c.Dir = abs
						if out, runErr := c.CombinedOutput(); runErr != nil {
							_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "git init failed: %v: %s\n", runErr, strings.TrimSpace(string(out)))
						} else {
							_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Ran git init")
						}
					}
				}
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nNext:\n  cd %s\n  heroctl login\n  heroctl deploy\n", dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "App name (default: the directory name)")
	cmd.Flags().StringVar(&lang, "lang", "static", "Language of the starter app: "+strings.Join(initLanguages(), ", "))
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite files that exist already")
	cmd.Flags().BoolVar(&noGit, "no-git", false, "Do not run git init")
	return cmd
}

func initLanguages() []string {
	langs := make([]string, 0, len(initTemplates))
	for l := range initTemplates {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

// initSafeName turns a directory name into an app name: lower case, hyphens for anything else.
func initSafeName(base string) string {
	var b strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(base) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		case !lastHyphen:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) > 40 {
		name = strings.Trim(name[:40], "-")
	}
	if name == "" {
		return "my-app"
	}
	return name
}

// initFiles is every file of a new repository, by relative path.
func initFiles(name string, t initTemplate) map[string]string {
	fill := strings.NewReplacer("@NAME@", name, "@PORT@", fmt.Sprint(t.port))
	files := map[string]string{
		"hero.toml":               fill.Replace(initHeroToml),
		"Dockerfile":              t.dockerfile,
		".dockerignore":           t.ignore,
		".gitignore":              initGitignore,
		"README.md":               fill.Replace(initReadme),
		".pre-commit-config.yaml": initPreCommit,
		filepath.Join(".github", "workflows", "deploy.yml"):  ciWorkflow(deployWorkflow, name, "main", ""),
		filepath.Join(".github", "workflows", "preview.yml"): ciWorkflow(previewWorkflow, name, "main", ""),
	}
	for rel, content := range t.source {
		files[rel] = fill.Replace(content)
	}
	return files
}
