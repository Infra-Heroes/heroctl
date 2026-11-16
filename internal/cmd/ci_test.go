package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inTempApp(t *testing.T, withToml bool) {
	t.Helper()
	dir := t.TempDir()
	if withToml {
		if err := os.WriteFile(filepath.Join(dir, "hero.toml"), []byte(shopTOML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
}

func runCI(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	cmd := ciCmd(newTestDeps(srv))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"github"}, args...))
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return out.String(), err
}

func tokenServer(t *testing.T, created *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/tokens" {
			*created++
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "tok-1", "name": "github-x", "scope": "deploy", "token": "hero_pat_secret"})
			return
		}
		http.NotFound(w, r)
	}))
}

func TestCIGitHub_WritesTheDeployWorkflow(t *testing.T) {
	inTempApp(t, true)
	n := 0
	srv := tokenServer(t, &n)
	defer srv.Close()
	out, err := runCI(t, srv, "--project", "my-project")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(".github/workflows/deploy.yml")
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"branches: [main]", "project: my-project", "infra-heroes/deploy-action@v1", "${{ secrets.HERO_TOKEN }}", "actions/checkout@v4"} {
		if !strings.Contains(got, want) {
			t.Errorf("workflow lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "image:") || strings.Contains(got, "@PROJECT@") || strings.Contains(got, "@BRANCH@") || strings.Contains(got, "@IMAGE@") {
		t.Errorf("unfilled placeholder or stray image line:\n%s", got)
	}
	if _, err := os.Stat(".github/workflows/preview.yml"); err == nil {
		t.Error("a preview workflow was written without --preview")
	}
	if n != 0 {
		t.Error("a token was created without --set-secret")
	}
	if !strings.Contains(out, "heroctl tokens create") || !strings.Contains(out, "HERO_TOKEN") {
		t.Errorf("it must say how to get the secret: %q", out)
	}
}

func TestCIGitHub_PreviewBranchAndImage(t *testing.T) {
	inTempApp(t, true)
	srv := tokenServer(t, new(int))
	defer srv.Close()
	if _, err := runCI(t, srv, "--project", "p", "--branch", "release/1", "--image", "docker.io/library/nginx:1.31", "--preview"); err != nil {
		t.Fatal(err)
	}
	deploy, _ := os.ReadFile(".github/workflows/deploy.yml")
	preview, _ := os.ReadFile(".github/workflows/preview.yml")
	if !strings.Contains(string(deploy), "branches: [release/1]") || !strings.Contains(string(deploy), "image: docker.io/library/nginx:1.31") {
		t.Errorf("deploy:\n%s", deploy)
	}
	for _, want := range []string{"pull_request", "closed", "pull-requests: write", "preview: true", "image: docker.io/library/nginx:1.31", "if: github.event.action != 'closed'"} {
		if !strings.Contains(string(preview), want) {
			t.Errorf("preview lacks %q:\n%s", want, preview)
		}
	}
}

func TestCIGitHub_RefusesWhatCouldBreakTheYAMLOrOverwrite(t *testing.T) {
	inTempApp(t, true)
	srv := tokenServer(t, new(int))
	defer srv.Close()
	for _, args := range [][]string{
		{"--project", "p\nrun: evil"},
		{"--project", "p: x"},
		{"--project", "p", "--branch", "main]\n  x: ["},
		{"--project", "p", "--image", "img\nrun: x"},
		{"--project", "p", "--image", "img with space"},
	} {
		if _, err := runCI(t, srv, args...); err == nil {
			t.Errorf("%q must be refused", args)
		}
	}
	if _, err := os.Stat(".github"); err == nil {
		t.Error("a refused call wrote files")
	}
	if _, err := runCI(t, srv, "--project", "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCI(t, srv, "--project", "p"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("an existing file must not be overwritten silently: %v", err)
	}
	if _, err := runCI(t, srv, "--project", "p", "--force"); err != nil {
		t.Errorf("--force: %v", err)
	}
}

func TestCIGitHub_NeedsHeroToml(t *testing.T) {
	inTempApp(t, false)
	srv := tokenServer(t, new(int))
	defer srv.Close()
	if _, err := runCI(t, srv, "--project", "p"); err == nil || !strings.Contains(err.Error(), "hero.toml") {
		t.Errorf("err = %v", err)
	}
}

func TestCIGitHub_SetSecretCreatesATokenAndPassesItOnlyToGH(t *testing.T) {
	inTempApp(t, true)
	n := 0
	srv := tokenServer(t, &n)
	defer srv.Close()
	var gotName, gotValue string
	old := setGitHubSecret
	setGitHubSecret = func(_ context.Context, name, value string) error { gotName, gotValue = name, value; return nil }
	t.Cleanup(func() { setGitHubSecret = old })
	out, err := runCI(t, srv, "--project", "p", "--set-secret")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || gotName != "HERO_TOKEN" || gotValue != "hero_pat_secret" {
		t.Errorf("tokens created %d, secret %q = %q", n, gotName, gotValue)
	}
	if strings.Contains(out, "hero_pat_secret") {
		t.Errorf("the token must never be printed: %q", out)
	}
	b, _ := os.ReadFile(".github/workflows/deploy.yml")
	if strings.Contains(string(b), "hero_pat_secret") {
		t.Error("the token must never be written to a file")
	}
}

func TestCIGitHub_ASecretThatCannotBeSetTellsHowToCleanUp(t *testing.T) {
	inTempApp(t, true)
	srv := tokenServer(t, new(int))
	defer srv.Close()
	old := setGitHubSecret
	setGitHubSecret = func(context.Context, string, string) error { return errors.New("gh is not installed") }
	t.Cleanup(func() { setGitHubSecret = old })
	_, err := runCI(t, srv, "--project", "p", "--set-secret")
	if err == nil || !strings.Contains(err.Error(), "tokens delete tok-1") || strings.Contains(err.Error(), "hero_pat_secret") {
		t.Errorf("err = %v", err)
	}
}
