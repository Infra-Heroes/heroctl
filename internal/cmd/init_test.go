package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/toml"
)

func runInit(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	cmd := initCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return out.String(), err
}

func TestInitWritesEverythingForEveryLanguage(t *testing.T) {
	for _, lang := range initLanguages() {
		t.Run(lang, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "My_Shop App")
			if _, err := runInit(t, dir, "--lang", lang, "--no-git"); err != nil {
				t.Fatal(err)
			}
			for _, rel := range []string{
				"hero.toml", "Dockerfile", ".dockerignore", ".gitignore", "README.md",
				".pre-commit-config.yaml", ".github/workflows/deploy.yml", ".github/workflows/preview.yml",
			} {
				if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
					t.Errorf("%s missing: %v", rel, err)
				}
			}
			raw, err := os.ReadFile(filepath.Join(dir, "hero.toml"))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := toml.Parse(strings.NewReader(string(raw)))
			if err != nil {
				t.Fatalf("generated hero.toml is invalid: %v", err)
			}
			if cfg.App.Name != "my-shop-app" {
				t.Errorf("app name = %q, want my-shop-app (from the directory name)", cfg.App.Name)
			}
			readme, _ := os.ReadFile(filepath.Join(dir, "README.md"))
			if strings.Contains(string(readme), "@NAME@") {
				t.Error("a placeholder was left in README.md")
			}
		})
	}
}

func TestInitNeverOverwritesWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runInit(t, dir, "--no-git")
	if err == nil || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("expected a refusal naming README.md, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(got) != "mine" {
		t.Error("the existing file was changed")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "hero.toml")); statErr == nil {
		t.Error("half a setup was written")
	}
	if _, err := runInit(t, dir, "--no-git", "--force"); err != nil {
		t.Fatal(err)
	}
}

func TestInitRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	if _, err := runInit(t, dir, "--lang", "cobol", "--no-git"); err == nil {
		t.Error("an unknown language was accepted")
	}
	if _, err := runInit(t, dir, "--name", "Bad Name!", "--no-git"); err == nil {
		t.Error("a bad app name was accepted")
	}
}

func TestInitSafeName(t *testing.T) {
	cases := map[string]string{"My_Shop App": "my-shop-app", "--x--": "x", "": "my-app", "\u00c4\u00d6": "my-app", "a.b": "a-b"}
	for in, want := range cases {
		if got := initSafeName(in); got != want {
			t.Errorf("initSafeName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := initSafeName(strings.Repeat("a", 60)); len(got) != 40 {
		t.Errorf("long name not shortened: %d", len(got))
	}
}

func TestInitNeedsNoLogin(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"init"})
	if err != nil || cmd == nil {
		t.Fatal("init is not a command")
	}
	if needsAuth(cmd) {
		t.Error("init must work without a stored token")
	}
}
