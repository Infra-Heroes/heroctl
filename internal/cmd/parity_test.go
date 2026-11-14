package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// ── pure helpers ─────────────────────────────────────────────────────────────

func TestFormatMetric(t *testing.T) {
	for _, tc := range []struct {
		v    float64
		unit string
		want string
	}{
		{12.345, "percent", "12.3 %"},
		{0, "percent", "0.0 %"},
		{512, "bytes", "512 B"},
		{2048, "bytes", "2.0 KiB"},
		{74457088, "bytes", "71.0 MiB"},
		{3 * 1024 * 1024 * 1024, "bytes", "3.0 GiB"},
		{1536, "bytes_per_second", "1.5 KiB/s"},
		{4.5, "", "4.50"},
	} {
		if got := formatMetric(tc.v, tc.unit); got != tc.want {
			t.Errorf("formatMetric(%v, %q) = %q, want %q", tc.v, tc.unit, got, tc.want)
		}
	}
}

func TestSparkline(t *testing.T) {
	if got := sparkline([]float64{1, 2, 3, 4, 5, 6, 7, 8}, 24); got != "▁▂▃▄▅▆▇█" {
		t.Errorf("rising values = %q", got)
	}
	if got := sparkline([]float64{5, 5, 5}, 24); got != "▁▁▁" {
		t.Errorf("a flat course must not invent a shape, got %q", got)
	}
	if got := sparkline(nil, 24); got != "" {
		t.Errorf("no values = %q", got)
	}
	long := make([]float64, 240)
	for i := range long {
		long[i] = float64(i)
	}
	if n := len([]rune(sparkline(long, 24))); n != 24 {
		t.Errorf("a long series must be shortened to the width, got %d characters", n)
	}
}

func TestBuildAlertRequest(t *testing.T) {
	t.Run("cpu", func(t *testing.T) {
		r, err := buildAlertRequest("cpu_percent", 80, true, 300, true, false, false)
		if err != nil || r.Threshold != 80 || r.ForSeconds == nil || *r.ForSeconds != 300 || r.Enabled != nil {
			t.Fatalf("req=%+v err=%v", r, err)
		}
	})
	t.Run("memory is asked in megabytes and sent in bytes", func(t *testing.T) {
		r, err := buildAlertRequest("memory_bytes", 400, true, 0, false, false, false)
		if err != nil || r.Threshold != 400*1024*1024 {
			t.Fatalf("req=%+v err=%v", r, err)
		}
	})
	t.Run("unavailable has no threshold", func(t *testing.T) {
		if _, err := buildAlertRequest("unavailable", 1, true, 0, false, false, false); err == nil {
			t.Error("a threshold on unavailable must be refused")
		}
		if _, err := buildAlertRequest("unavailable", 0, false, 0, false, false, false); err != nil {
			t.Errorf("unavailable without a threshold must work: %v", err)
		}
	})
	t.Run("off", func(t *testing.T) {
		r, err := buildAlertRequest("cpu_percent", 90, true, 0, false, true, true)
		if err != nil || r.Enabled == nil || *r.Enabled {
			t.Fatalf("req=%+v err=%v", r, err)
		}
	})
	for name, args := range map[string]struct {
		metric string
		thr    float64
		has    bool
	}{
		"cpu without threshold":    {"cpu_percent", 0, false},
		"cpu above 100":            {"cpu_percent", 150, true},
		"cpu zero":                 {"cpu_percent", 0, true},
		"memory without threshold": {"memory_bytes", 0, false},
		"unknown metric":           {"disk", 1, true},
	} {
		t.Run("refused: "+name, func(t *testing.T) {
			if _, err := buildAlertRequest(args.metric, args.thr, args.has, 0, false, false, false); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestParsePairs(t *testing.T) {
	got, err := parsePairs([]string{"A=1", "URL=https://x?a=b", "EMPTY="})
	if err != nil || got["A"] != "1" || got["URL"] != "https://x?a=b" || got["EMPTY"] != "" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	for _, bad := range []string{"NOEQUALS", "=value", ""} {
		if _, err := parsePairs([]string{bad}); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestMatchEnd(t *testing.T) {
	ids := []string{"01a1-aaaa-0000000a1b2c", "01a1-bbbb-0000000c3d4e", "01a1-cccc-1111111c3d4e"}
	if got, err := matchEnd(ids, "a1b2c", "alert"); err != nil || got != ids[0] {
		t.Errorf("unique end: %v %v", got, err)
	}
	if _, err := matchEnd(ids, "c3d4e", "alert"); err == nil || !strings.Contains(err.Error(), "2 alerts end with") {
		t.Errorf("ambiguous end: %v", err)
	}
	if _, err := matchEnd(ids, "zzzzz", "alert"); err == nil {
		t.Error("unknown end must fail")
	}
	if _, err := matchEnd(ids, "3d", "alert"); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("too short: %v", err)
	}
}

func TestFilterLogs(t *testing.T) {
	in := "start\nGET /a 200\nERROR boom\nGET /b 200\nerror again\nlast\n"
	run := func(tail int, pattern string) string {
		var re *regexp.Regexp
		if pattern != "" {
			re = regexp.MustCompile(pattern)
		}
		var out bytes.Buffer
		if err := filterLogs(strings.NewReader(in), &out, tail, re); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := run(2, ""); got != "error again\nlast\n" {
		t.Errorf("tail 2 = %q", got)
	}
	if got := run(0, "ERROR|error"); got != "ERROR boom\nerror again\n" {
		t.Errorf("grep = %q", got)
	}
	if got := run(1, "GET"); got != "GET /b 200\n" {
		t.Errorf("tail of the matching lines = %q", got)
	}
	if got := run(0, "nothing matches"); got != "" {
		t.Errorf("no match = %q", got)
	}
	// A last line without a newline is still a line.
	var out bytes.Buffer
	if err := filterLogs(strings.NewReader("a\nb"), &out, 0, regexp.MustCompile("b")); err != nil || out.String() != "b" {
		t.Errorf("unterminated last line: %q %v", out.String(), err)
	}
	// A line far beyond bufio.Scanner's 64KB must not stop the stream.
	huge := strings.Repeat("x", 200*1024)
	out.Reset()
	if err := filterLogs(strings.NewReader("short\n"+huge+"\nafter\n"), &out, 1, nil); err != nil || out.String() != "after\n" {
		t.Errorf("a very long line broke the stream: %v", err)
	}
}

func TestCompileGrep(t *testing.T) {
	if re, err := compileGrep(""); err != nil || re != nil {
		t.Errorf("empty pattern must mean no filter, got %v %v", re, err)
	}
	if _, err := compileGrep("("); err == nil {
		t.Error("an invalid pattern must be an error")
	}
}

// ── commands against a stub API ──────────────────────────────────────────────

type opsServer struct {
	calls   []string
	bodies  map[string]map[string]any
	env     map[string]string
	alerts  []client.DeploymentAlert
	volSize int
}

func newOpsServer(t *testing.T, s *opsServer) *httptest.Server {
	t.Helper()
	s.bodies = map[string]map[string]any{}
	if s.volSize == 0 {
		s.volSize = 10
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		s.calls = append(s.calls, key)
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			s.bodies[key] = m
		}
		switch {
		case key == "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case key == "GET /api/v1/projects/proj-1/deployments/web":
			_ = json.NewEncoder(w).Encode(map[string]any{"app_name": "web", "env": s.env})
		case key == "POST /api/v1/projects/proj-1/deployments/web/env":
			_ = json.NewEncoder(w).Encode(map[string]any{"app_name": "web"})
		case key == "GET /api/v1/projects/proj-1/deployments/web/alerts":
			_ = json.NewEncoder(w).Encode(map[string]any{"alerts": s.alerts})
		case key == "PUT /api/v1/projects/proj-1/deployments/web/alerts":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "x", "metric": s.bodies[key]["metric"], "threshold": s.bodies[key]["threshold"], "enabled": true})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/alerts/"):
			w.WriteHeader(http.StatusNoContent)
		case key == "GET /api/v1/projects/proj-1/volumes":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "vol-1", "name": "data", "size_gb": s.volSize, "status": "available"}})
		case key == "POST /api/v1/projects/proj-1/volumes/vol-1/resize":
			_ = json.NewEncoder(w).Encode(map[string]any{"size_gb": s.bodies[key]["size_gb"]})
		case key == "GET /api/v1/projects/proj-1/deployments/web/metrics":
			_ = json.NewEncoder(w).Encode(map[string]any{"allocations": 1, "range": "1h", "series": []map[string]any{
				{"name": "cpu", "unit": "percent", "points": []map[string]any{{"T": 1, "V": 10.0}, {"T": 2, "V": 30.0}}},
				{"name": "memory", "unit": "bytes", "points": []map[string]any{}},
			}})
		case key == "GET /api/v1/projects/proj-1/deployments/web/events":
			_ = json.NewEncoder(w).Encode(map[string]any{"events": []map[string]any{{"action": "restart", "outcome": "ok", "created_at": "2026-11-14T12:00:00Z"}}, "total": 1})
		case key == "GET /api/v1/audit":
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []map[string]any{
				{"action": "api.request", "actor": "u1", "created_at": "2026-11-14T12:03:00Z"},
				{"action": "api.request", "actor": "u1", "created_at": "2026-11-14T12:02:00Z"},
				{"action": "deployment.created", "actor": "u1", "created_at": "2026-11-14T12:01:00Z"},
				{"action": "api.denied", "actor": "u2", "created_at": "2026-11-14T12:00:00Z"},
			}, "total": 4})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func execCmd(t *testing.T, cmd *cobra.Command, args ...string) error {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd.Execute()
}

func called(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func TestEnvSet_MergesIntoTheWholeEnvironment(t *testing.T) {
	s := &opsServer{env: map[string]string{"KEEP": "1", "DB": "secret:DB_PASSWORD", "LOG": "info"}}
	srv := newOpsServer(t, s)
	if err := execCmd(t, deploymentsEnvCmd(newTestDeps(srv)), "set", "web", "LOG=debug", "NEW=x", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	env, _ := s.bodies["POST /api/v1/projects/proj-1/deployments/web/env"]["env"].(map[string]any)
	want := map[string]string{"KEEP": "1", "DB": "secret:DB_PASSWORD", "LOG": "debug", "NEW": "x"}
	if len(env) != len(want) {
		t.Fatalf("sent %v, want all of %v: the API replaces the environment", env, want)
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %v, want %s (secret references must survive untouched)", k, env[k], v)
		}
	}
}

func TestEnvUnset(t *testing.T) {
	s := &opsServer{env: map[string]string{"A": "1", "B": "2"}}
	srv := newOpsServer(t, s)
	if err := execCmd(t, deploymentsEnvCmd(newTestDeps(srv)), "unset", "web", "A", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	env, _ := s.bodies["POST /api/v1/projects/proj-1/deployments/web/env"]["env"].(map[string]any)
	if len(env) != 1 || env["B"] != "2" {
		t.Fatalf("sent %v, want only B", env)
	}
}

func TestEnvUnset_UnknownKeySendsNothing(t *testing.T) {
	s := &opsServer{env: map[string]string{"A": "1"}}
	srv := newOpsServer(t, s)
	if err := execCmd(t, deploymentsEnvCmd(newTestDeps(srv)), "unset", "web", "NOPE", "--project", "app"); err == nil {
		t.Fatal("expected an error for a variable that is not set")
	}
	if called(s.calls, "POST /api/v1/projects/proj-1/deployments/web/env") {
		t.Error("nothing may be sent when the change fails")
	}
}

func TestEnvSet_RefusesToWriteBackMaskedValues(t *testing.T) {
	s := &opsServer{env: map[string]string{"OLD": "***"}}
	srv := newOpsServer(t, s)
	err := execCmd(t, deploymentsEnvCmd(newTestDeps(srv)), "set", "web", "NEW=1", "--project", "app")
	if err == nil || !strings.Contains(err.Error(), "cannot be read back") {
		t.Fatalf("err = %v, want the masked-values refusal", err)
	}
	if called(s.calls, "POST /api/v1/projects/proj-1/deployments/web/env") {
		t.Error("writing back *** would turn every value into the literal text")
	}
}

func TestEnvSet_RefusesInvalidKeys(t *testing.T) {
	s := &opsServer{env: map[string]string{}}
	srv := newOpsServer(t, s)
	if err := execCmd(t, deploymentsEnvCmd(newTestDeps(srv)), "set", "web", "1BAD=x", "--project", "app"); err == nil {
		t.Fatal("an invalid key must be refused")
	}
	if called(s.calls, "POST /api/v1/projects/proj-1/deployments/web/env") {
		t.Error("nothing may be sent for an invalid key")
	}
}

func TestAlertsSet_SendsMemoryInBytes(t *testing.T) {
	s := &opsServer{}
	srv := newOpsServer(t, s)
	if err := execCmd(t, deploymentsAlertsCmd(newTestDeps(srv)), "set", "web", "--project", "app", "--metric", "memory_bytes", "--threshold", "400", "--for", "120"); err != nil {
		t.Fatal(err)
	}
	b := s.bodies["PUT /api/v1/projects/proj-1/deployments/web/alerts"]
	if b["metric"] != "memory_bytes" || b["threshold"] != float64(400*1024*1024) || b["for_seconds"] != float64(120) {
		t.Errorf("body = %v", b)
	}
}

func TestAlertsDelete_ByEndOfID(t *testing.T) {
	s := &opsServer{alerts: []client.DeploymentAlert{
		{ID: "01a1d9e6-aaaa-7000-8000-00000000a1b2", Metric: "cpu_percent"},
		{ID: "01a1d9e6-bbbb-7000-8000-00000000c3d4", Metric: "memory_bytes"},
	}}
	srv := newOpsServer(t, s)
	if err := execCmd(t, deploymentsAlertsCmd(newTestDeps(srv)), "delete", "web", "c3d4", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	if !called(s.calls, "DELETE /api/v1/projects/proj-1/deployments/web/alerts/01a1d9e6-bbbb-7000-8000-00000000c3d4") {
		t.Errorf("calls = %v", s.calls)
	}
}

func TestVolumesResize(t *testing.T) {
	t.Run("grows", func(t *testing.T) {
		s := &opsServer{volSize: 10}
		srv := newOpsServer(t, s)
		if err := execCmd(t, volumesResizeCmd(newTestDeps(srv)), "data", "--project", "app", "--size", "20"); err != nil {
			t.Fatal(err)
		}
		if got := s.bodies["POST /api/v1/projects/proj-1/volumes/vol-1/resize"]["size_gb"]; got != float64(20) {
			t.Errorf("size_gb = %v", got)
		}
	})
	t.Run("never shrinks", func(t *testing.T) {
		s := &opsServer{volSize: 10}
		srv := newOpsServer(t, s)
		err := execCmd(t, volumesResizeCmd(newTestDeps(srv)), "data", "--project", "app", "--size", "5")
		if err == nil || !strings.Contains(err.Error(), "cannot be made smaller") {
			t.Fatalf("err = %v", err)
		}
		if called(s.calls, "POST /api/v1/projects/proj-1/volumes/vol-1/resize") {
			t.Error("a shrink must not be sent")
		}
	})
	t.Run("same size is a no-op", func(t *testing.T) {
		s := &opsServer{volSize: 10}
		srv := newOpsServer(t, s)
		if err := execCmd(t, volumesResizeCmd(newTestDeps(srv)), "data", "--project", "app", "--size", "10"); err != nil {
			t.Fatal(err)
		}
		if called(s.calls, "POST /api/v1/projects/proj-1/volumes/vol-1/resize") {
			t.Error("nothing to send for the same size")
		}
	})
}

func TestMetricsEventsAuditRead(t *testing.T) {
	s := &opsServer{}
	srv := newOpsServer(t, s)
	deps := newTestDeps(srv)
	if err := execCmd(t, deploymentsMetricsCmd(deps), "web", "--project", "app", "--range", "24h"); err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if err := execCmd(t, deploymentsEventsCmd(deps), "web", "--project", "app"); err != nil {
		t.Fatalf("events: %v", err)
	}
	if err := execCmd(t, auditCmd(deps)); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if !called(s.calls, "GET /api/v1/projects/proj-1/deployments/web/metrics") || !called(s.calls, "GET /api/v1/audit") {
		t.Errorf("calls = %v", s.calls)
	}
}

// ── deploy --image ───────────────────────────────────────────────────────────

func TestDeployWithImageBuildsNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hero.toml"), []byte("[app]\nname = \"web\"\n\n[deploy]\ncpu = 1\nmemory_mb = 256\nport = 80\nhealth_path = \"/\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	// With no container engine on the PATH a build would fail; --image must not need one.
	t.Setenv("PATH", t.TempDir())

	var gotImage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		switch key {
		case "GET /api/v1/orgs/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "org"})
		case "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case "POST /api/v1/projects/proj-1/deployments":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			gotImage, _ = b["image"].(string)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"app_name": "web", "image": gotImage, "hostname": "web.example"})
		case "GET /api/v1/projects/proj-1/deployments/web/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"nomad_status": "running", "healthy": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	err := execCmd(t, deployCmd(newTestDeps(srv)), "--project", "app", "--image", "docker.io/library/nginx:1.31", "--yes")
	if err != nil {
		t.Fatalf("deploy --image: %v", err)
	}
	if gotImage != "docker.io/library/nginx:1.31" {
		t.Errorf("deployed image = %q, want the one given", gotImage)
	}
}

func TestDeployImageWithWhitespaceIsRefused(t *testing.T) {
	err := execCmd(t, deployCmd(&Deps{}), "--project", "app", "--image", "nginx 1.31")
	if err == nil || !strings.Contains(err.Error(), "whitespace") {
		t.Fatalf("err = %v", err)
	}
}

func TestAudit_LeavesOutPerRequestNoiseUnlessAsked(t *testing.T) {
	s := &opsServer{}
	srv := newOpsServer(t, s)
	deps := newTestDeps(srv)

	got, err := collectAudit(context.Background(), deps, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Action != "deployment.created" || got[1].Action != "api.denied" {
		t.Errorf("entries = %+v, want the deploy and the denial, without api.request", got)
	}

	all, err := collectAudit(context.Background(), deps, 10, true)
	if err != nil || len(all) != 4 {
		t.Errorf("--all must keep every entry, got %d (%v)", len(all), err)
	}

	one, err := collectAudit(context.Background(), deps, 1, false)
	if err != nil || len(one) != 1 || one[0].Action != "deployment.created" {
		t.Errorf("limit 1 = %+v (%v)", one, err)
	}
}
