package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/workspace"
)

func runWS(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestWorkspaceLifecycle(t *testing.T) {
	d := t.TempDir()
	old := workspace.Dir
	workspace.Dir = func() (string, error) { return d, nil }
	t.Cleanup(func() { workspace.Dir = old })
	t.Setenv("HERO_WORKSPACE", "")

	if _, err := runWS(t, "workspace", "create", "Bad Name"); err == nil {
		t.Fatal("an invalid name must be refused")
	}
	if out, err := runWS(t, "workspace", "create", "acme", "--project", "shop", "--use"); err != nil || !strings.Contains(out, "heroctl login") {
		t.Fatalf("create: %v %s", err, out)
	}
	if _, err := runWS(t, "workspace", "create", "acme"); err == nil {
		t.Fatal("a duplicate must be refused")
	}
	if out, _ := runWS(t, "workspace", "current"); !strings.Contains(out, "acme (project shop") {
		t.Fatalf("current: %s", out)
	}
	if err := os.MkdirAll(filepath.Join(d, "tokens"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "tokens", "acme.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runWS(t, "workspace", "list")
	if err != nil || !strings.Contains(out, "*  acme") || !strings.Contains(out, "default") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if _, err := runWS(t, "workspace", "set", "acme", "--project", "blog"); err != nil {
		t.Fatal(err)
	}
	if out, _ := runWS(t, "workspace", "current"); !strings.Contains(out, "project blog") {
		t.Fatalf("after set: %s", out)
	}
	if _, err := runWS(t, "workspace", "use", "nope"); err == nil {
		t.Fatal("using an unknown workspace must fail")
	}
	if _, err := runWS(t, "workspace", "delete", "default"); err == nil {
		t.Fatal("default cannot be deleted")
	}
	if _, err := runWS(t, "workspace", "delete", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "tokens", "acme.json")); !os.IsNotExist(err) {
		t.Error("the login of a deleted workspace must go with it")
	}
	if out, _ := runWS(t, "workspace", "current"); !strings.HasPrefix(out, "default") {
		t.Fatalf("after delete: %s", out)
	}
	if _, err := runWS(t, "workspace", "set", "nope"); err == nil {
		t.Fatal("set on unknown must fail")
	}
	if _, err := runWS(t, "workspace", "delete", "nope"); err == nil {
		t.Fatal("delete unknown must fail")
	}
}

func TestWorkspaceFillsProjectAndPicksTheLogin(t *testing.T) {
	d := t.TempDir()
	old := workspace.Dir
	workspace.Dir = func() (string, error) { return d, nil }
	t.Cleanup(func() { workspace.Dir = old })
	f, _ := workspace.Load()
	f.Workspaces["acme"] = workspace.Workspace{Project: "shop"}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERO_WORKSPACE", "acme")

	root := newRootCmd()
	cmd := findCmd(t, root, "secrets", "list")
	if _, err := activateWorkspace(cmd, ""); err != nil {
		t.Fatal(err)
	}
	if got := cmd.Flags().Lookup("project").Value.String(); got != "shop" {
		t.Errorf("project = %q", got)
	}
	// An explicit --project wins.
	cmd2 := findCmd(t, newRootCmd(), "secrets", "list")
	_ = cmd2.Flags().Set("project", "other")
	if _, err := activateWorkspace(cmd2, ""); err != nil {
		t.Fatal(err)
	}
	if got := cmd2.Flags().Lookup("project").Value.String(); got != "other" {
		t.Errorf("explicit project lost: %q", got)
	}
	if _, err := activateWorkspace(cmd2, "missing"); err == nil {
		t.Error("an unknown workspace must be an error")
	}
}
