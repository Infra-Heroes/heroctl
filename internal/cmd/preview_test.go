package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/toml"
)

func parseTOML(t *testing.T, s string) *toml.HeroConfig {
	t.Helper()
	cfg, err := toml.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const shopTOML = `
[app]
name = "shop"
custom_domains = ["shop.example.com"]

[deploy]
cpu = 1
memory_mb = 256
port = 8080
health_path = "/"

[[volumes]]
name = "uploads"
mount = "/data"

[[databases]]
name = "db"
engine = "postgres"
`

func TestApplyPreview_RenamesAndLeavesOutWhatBelongsToTheRealApp(t *testing.T) {
	cfg := parseTOML(t, shopTOML)
	cfg.Labels = map[string]string{"hero-custom-domains": "shop.example.com", "team": "a"}
	notes, err := applyPreview(cfg, "pr-12")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Labels["hero-preview"] != "true" {
		t.Errorf("a preview carries hero-preview=true (what a deploy token may delete): %v", cfg.Labels)
	}
	if cfg.App.Name != "shop-pr-12" {
		t.Errorf("name = %s", cfg.App.Name)
	}
	if len(cfg.App.CustomDomains) != 0 || cfg.Labels["hero-custom-domains"] != "" || cfg.Labels["team"] != "a" {
		t.Errorf("domains %v labels %v", cfg.App.CustomDomains, cfg.Labels)
	}
	if len(cfg.Volumes) != 0 {
		t.Error("a preview must never attach the real app's volume")
	}
	if !cfg.Deploy.ScaleToZero {
		t.Error("a preview should scale to zero")
	}
	if len(cfg.Databases) != 1 {
		t.Error("the databases stay: the preview creates its own under its own name")
	}
	if len(notes) != 3 {
		t.Errorf("notes = %v", notes)
	}
}

func TestPreviewAppName_RefusesWhatCouldHitTheRealAppOrBreakTheName(t *testing.T) {
	for _, bad := range []string{"", "PR-1", "-pr", "pr-", "pr 1", "pr/1", "../x", strings.Repeat("a", 33)} {
		if _, err := previewAppName("shop", bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if _, err := previewAppName(strings.Repeat("a", 60), "pr-12"); err == nil {
		t.Error("a name over 63 characters must be refused")
	}
	if got, err := previewAppName("shop", "pr-12"); err != nil || got != "shop-pr-12" {
		t.Errorf("%q %v", got, err)
	}
}

func previewServer(t *testing.T, calls *[]string, withPreview bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		*calls = append(*calls, key)
		switch {
		case key == "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case key == "GET /api/v1/projects/proj-1/deployments":
			ds := []map[string]any{
				{"app_name": "shop", "status": "running"},
				{"app_name": "shop-db", "labels": map[string]string{"hero-managed-db": "postgres", "hero-db-for": "shop"}},
			}
			if withPreview {
				ds = append(ds,
					map[string]any{"app_name": "shop-pr-7", "status": "running"},
					map[string]any{"app_name": "shop-pr-7-db", "labels": map[string]string{"hero-managed-db": "postgres", "hero-db-for": "shop-pr-7"}})
			}
			_ = json.NewEncoder(w).Encode(ds)
		case key == "GET /api/v1/projects/proj-1/volumes":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
}

func runPreviewDelete(t *testing.T, srv *httptest.Server, name string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/hero.toml", []byte(shopTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var out strings.Builder
	cmd := previewCmd(newTestDeps(srv))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"delete", name, "--project", "app"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return out.String(), err
}

func TestPreviewDelete_RemovesThePreviewAndItsDatabaseOnly(t *testing.T) {
	var calls []string
	srv := previewServer(t, &calls, true)
	defer srv.Close()
	if _, err := runPreviewDelete(t, srv, "pr-7"); err != nil {
		t.Fatal(err)
	}
	got := deleted(calls)
	want := map[string]bool{
		"DELETE /api/v1/projects/proj-1/deployments/shop-pr-7":    true,
		"DELETE /api/v1/projects/proj-1/deployments/shop-pr-7-db": true,
	}
	for _, c := range got {
		if strings.Contains(c, "/secrets/") {
			continue
		}
		if !want[c] {
			t.Errorf("deleted something that is not the preview: %s", c)
		}
		delete(want, c)
	}
	if len(want) != 0 {
		t.Errorf("not deleted: %v", want)
	}
}

func TestPreviewDelete_APreviewThatIsGoneIsNotAnError(t *testing.T) {
	var calls []string
	srv := previewServer(t, &calls, false)
	defer srv.Close()
	if _, err := runPreviewDelete(t, srv, "pr-7"); err != nil {
		t.Fatalf("a closed PR whose preview never existed must not fail the cleanup job: %v", err)
	}
	if len(deleted(calls)) != 0 {
		t.Errorf("nothing exists, nothing may be deleted: %v", deleted(calls))
	}
}

func TestApplyPreview_AWorkerKeepsRunningInsteadOfScalingToZero(t *testing.T) {
	cfg := parseTOML(t, "[app]\nname = \"queue\"\n[deploy]\nworker = true\n")
	if _, err := applyPreview(cfg, "pr-3"); err != nil {
		t.Fatal(err)
	}
	if cfg.Deploy.ScaleToZero {
		t.Error("nothing would ever start a worker that scaled to zero")
	}
}

// A deploy-scoped token may delete the preview app and nothing else. The app must go
// even when the platform refuses its database, and the command must say that the
// database is still there, because it still costs.
func TestPreviewDelete_DeletesTheAppFirstAndReportsADatabaseItCouldNotDelete(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		calls = append(calls, key)
		switch {
		case key == "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case key == "GET /api/v1/projects/proj-1/deployments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"app_name": "shop-pr-7", "status": "running"},
				{"app_name": "shop-pr-7-db", "labels": map[string]string{"hero-managed-db": "postgres", "hero-db-for": "shop-pr-7"}},
			})
		case key == "DELETE /api/v1/projects/proj-1/deployments/shop-pr-7":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete:
			http.Error(w, `{"error":"token scope does not allow this action"}`, http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := runPreviewDelete(t, srv, "pr-7")
	if err == nil || !strings.Contains(err.Error(), "shop-pr-7-db") || !strings.Contains(err.Error(), "scope all") {
		t.Fatalf("err = %v", err)
	}
	if len(calls) < 3 || calls[2] != "DELETE /api/v1/projects/proj-1/deployments/shop-pr-7" {
		t.Errorf("the app must be deleted before its databases: %v", calls)
	}
}

func TestPreviewExpiry(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Duration{"72h": 72 * time.Hour, "7d": 7 * 24 * time.Hour, "90m": 90 * time.Minute} {
		got, err := previewExpiry(in, now)
		if err != nil || got.Sub(now) != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"soon", "0h", "-1h", "5m", "91d", "xd"} {
		if _, err := previewExpiry(bad, now); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}
