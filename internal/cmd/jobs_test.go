package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

const jobsTOML = `
[app]
name = "shop"

[deploy]
cpu = 1
memory_mb = 256
port = 8080
health_path = "/"

[env]
MODE = "prod"

[[jobs]]
name = "cleanup"
schedule = "0 3 * * *"
command = "python cleanup.py && echo done"
timezone = "Europe/Berlin"
timeout = "30m"

[[jobs]]
name = "report"
schedule = "*/15 * * * *"
command = "./report"
memory_mb = 2048
`

func TestJobRequest_IsAWorkerOfTheAppsImageWithTheCommandInTheEnvironment(t *testing.T) {
	cfg := parseTOML(t, jobsTOML)
	req := jobRequest(cfg, cfg.Jobs[0], "registry/x:1")
	if req.AppName != "shop-cleanup" || req.Image != "registry/x:1" || req.HealthCheckType != "none" || req.ServiceScope != "internal" {
		t.Errorf("req = %+v", req)
	}
	if req.Env["HERO_COMMAND"] != "python cleanup.py && echo done" || req.Env["MODE"] != "prod" {
		t.Errorf("env = %v", req.Env)
	}
	if cfg.Env["HERO_COMMAND"] != "" {
		t.Error("the app's own environment must not be changed by building a job")
	}
	for k, want := range map[string]string{
		"hero-job-for": "shop", "hero-job-name": "cleanup", "hero-job-schedule": "0 3 * * *",
		"hero-job-timezone": "Europe/Berlin", "hero-job-timeout": "30m",
	} {
		if req.Labels[k] != want {
			t.Errorf("label %s = %q, want %q", k, req.Labels[k], want)
		}
	}
	if req.MemoryMB != 256 || jobRequest(cfg, cfg.Jobs[1], "i").MemoryMB != 2048 {
		t.Error("a job takes the app's size unless it names its own")
	}
	if _, ok := jobRequest(cfg, cfg.Jobs[1], "i").Labels["hero-job-timezone"]; ok {
		t.Error("an unset timezone must not be sent")
	}
}

type jobsServer struct {
	created []client.CreateDeploymentRequest
	deleted []string
	ran     []string
}

func newJobsServer(t *testing.T, s *jobsServer, existing []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		switch {
		case key == "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case key == "GET /api/v1/projects/proj-1/deployments":
			_ = json.NewEncoder(w).Encode(existing)
		case key == "POST /api/v1/projects/proj-1/deployments":
			var req client.CreateDeploymentRequest
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &req)
			s.created = append(s.created, req)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "d1"})
		case r.Method == http.MethodDelete:
			s.deleted = append(s.deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/run"):
			s.ran = append(s.ran, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "started"})
		case strings.HasSuffix(r.URL.Path, "/runs"):
			_, _ = w.Write([]byte(`{"runs":[{"run":"a","started":"2026-11-16T03:00:00Z","ended":"2026-11-16T03:00:12Z","seconds":12,"exit_code":0},
			 {"run":"b","started":"2026-11-15T03:00:00Z","ended":"2026-11-15T03:00:05Z","seconds":5,"exit_code":3},
			 {"run":"c","started":"2026-11-16T04:00:00Z"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestDeployJobs_CreatesEveryJobAndRemovesTheOnesThatLeftHeroToml(t *testing.T) {
	s := &jobsServer{}
	srv := newJobsServer(t, s, []map[string]any{
		{"app_name": "shop-cleanup", "labels": map[string]string{"hero-job-for": "shop", "hero-job-schedule": "0 3 * * *", "hero-job-name": "cleanup"}},
		{"app_name": "shop-old", "labels": map[string]string{"hero-job-for": "shop", "hero-job-schedule": "0 1 * * *", "hero-job-name": "old"}},
		{"app_name": "other-job", "labels": map[string]string{"hero-job-for": "other", "hero-job-schedule": "0 1 * * *", "hero-job-name": "job"}},
		{"app_name": "shop"},
	})
	defer srv.Close()
	deps := newTestDeps(srv)
	if err := deployJobs(t.Context(), deps, &client.Project{ID: "proj-1", Name: "app"}, parseTOML(t, jobsTOML), "img:1"); err != nil {
		t.Fatal(err)
	}
	if len(s.created) != 2 || s.created[0].AppName != "shop-cleanup" || s.created[1].AppName != "shop-report" {
		t.Errorf("created = %+v", s.created)
	}
	if len(s.deleted) != 1 || !strings.HasSuffix(s.deleted[0], "/deployments/shop-old") {
		t.Errorf("deleted = %v: only the removed job of this app may go, never another app's job or the app", s.deleted)
	}
}

func TestDeployJobs_AnAppWithoutJobsMakesNoCallsThatChangeAnything(t *testing.T) {
	s := &jobsServer{}
	srv := newJobsServer(t, s, []map[string]any{{"app_name": "shop"}})
	defer srv.Close()
	cfg := parseTOML(t, "[app]\nname = \"shop\"\n[deploy]\nhealth_path = \"/\"\n")
	if err := deployJobs(t.Context(), newTestDeps(srv), &client.Project{ID: "proj-1"}, cfg, "img"); err != nil {
		t.Fatal(err)
	}
	if len(s.created)+len(s.deleted) != 0 {
		t.Errorf("created %v deleted %v", s.created, s.deleted)
	}
}

func runJobsCmd(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	cmd := jobsCmd(newTestDeps(srv))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return out.String(), err
}

func TestJobsList_ShowsOnlyScheduledJobs(t *testing.T) {
	s := &jobsServer{}
	srv := newJobsServer(t, s, []map[string]any{
		{"app_name": "shop", "status": "running"},
		{"app_name": "shop-cleanup", "status": "running", "labels": map[string]string{"hero-job-schedule": "0 3 * * *", "hero-job-timezone": "Europe/Berlin", "hero-job-timeout": "30m"}},
	})
	defer srv.Close()
	out, err := runJobsCmd(t, srv, "list", "--project", "app")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "shop-cleanup") || !strings.Contains(out, "0 3 * * *") || !strings.Contains(out, "Europe/Berlin") || strings.Contains(out, "\nshop ") {
		t.Errorf("out = %q", out)
	}
}

func TestJobsRun_StartsARunAndRunsShowsExitCodes(t *testing.T) {
	s := &jobsServer{}
	srv := newJobsServer(t, s, nil)
	defer srv.Close()
	if _, err := runJobsCmd(t, srv, "run", "shop-cleanup", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	if len(s.ran) != 1 || !strings.HasSuffix(s.ran[0], "/deployments/shop-cleanup/run") {
		t.Errorf("ran = %v", s.ran)
	}
	out, err := runJobsCmd(t, srv, "runs", "shop-cleanup", "--project", "app")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"12s", "5s", "running"} {
		if !strings.Contains(out, want) {
			t.Errorf("runs output lacks %q:\n%s", want, out)
		}
	}
	lines := strings.Split(out, "\n")
	var failed string
	for _, l := range lines {
		if strings.Contains(l, "5s") {
			failed = l
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(failed), "3") {
		t.Errorf("the failed run must show exit 3: %q", failed)
	}
}

func TestApplyPreview_DropsJobs(t *testing.T) {
	cfg := parseTOML(t, jobsTOML)
	notes, err := applyPreview(cfg, "pr-5")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Jobs) != 0 {
		t.Error("a preview must not run the app's scheduled jobs")
	}
	found := false
	for _, n := range notes {
		found = found || strings.Contains(n, "scheduled jobs")
	}
	if !found {
		t.Errorf("notes = %v", notes)
	}
}
