package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func useTempDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	old := Dir
	Dir = func() (string, error) { return d, nil }
	t.Cleanup(func() { Dir = old })
	return d
}

func TestLoadWithoutFileIsTheDefaultAlone(t *testing.T) {
	useTempDir(t)
	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Names(); len(got) != 1 || got[0] != Default {
		t.Fatalf("names %v", got)
	}
	name, _, err := f.Active("")
	if err != nil || name != Default {
		t.Fatalf("active %q %v", name, err)
	}
}

func TestSaveLoadAndActiveOrder(t *testing.T) {
	d := useTempDir(t)
	f, _ := Load()
	f.Workspaces["acme"] = Workspace{Project: "shop", Server: "https://api.example"}
	f.Current = "acme"
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(d, "workspaces.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	g, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if n, ws, _ := g.Active(""); n != "acme" || ws.Project != "shop" {
		t.Errorf("current: %s %+v", n, ws)
	}
	if n, _, _ := g.Active(Default); n != Default {
		t.Errorf("explicit must win: %s", n)
	}
	if _, _, err := g.Active("nope"); err == nil {
		t.Error("unknown workspace must be an error")
	}
}

func TestBrokenFileIsReported(t *testing.T) {
	d := useTempDir(t)
	if err := os.WriteFile(filepath.Join(d, "workspaces.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestDefaultKeepsTheOldTokenPath(t *testing.T) {
	d := useTempDir(t)
	if p, _ := TokenFile(Default); p != filepath.Join(d, "token.json") {
		t.Errorf("default token at %s", p)
	}
	if p, _ := TokenFile(""); p != filepath.Join(d, "token.json") {
		t.Errorf("empty token at %s", p)
	}
	if p, _ := TokenFile("acme"); p != filepath.Join(d, "tokens", "acme.json") {
		t.Errorf("acme token at %s", p)
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"a", "acme", "acme-prod", "x1"} {
		if ValidName(ok) != nil {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "Acme", "-a", "a-", "a b", "a/b"} {
		if ValidName(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestMissingFileDefaultAddedWhenAbsent(t *testing.T) {
	d := useTempDir(t)
	if err := os.WriteFile(filepath.Join(d, "workspaces.json"), []byte(`{"workspaces":{"x":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Workspaces[Default]; !ok {
		t.Error("default must always exist")
	}
}
