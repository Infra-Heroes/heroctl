package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func stagedServer(t *testing.T, seen *[]string, decideStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/projects"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "p1", "name": "shop"}})
		case strings.HasSuffix(r.URL.Path, "/staged"):
			_ = json.NewEncoder(w).Encode(map[string]any{"image": "web:2", "live_image": "web:1",
				"next_url": "https://web--next.heroapp.run", "since": "2026-11-18T10:00:00Z"})
		default:
			w.WriteHeader(decideStatus)
			_, _ = w.Write([]byte(`{"error":"no staged version is waiting"}`))
		}
	}))
}

func TestStagedCommands(t *testing.T) {
	var seen []string
	srv := stagedServer(t, &seen, http.StatusOK)
	defer srv.Close()
	deps := newTestDeps(srv)

	for _, c := range []struct {
		cmd  func() error
		want string
	}{
		{func() error {
			c := deploymentsStagedCmd(deps)
			c.SetArgs([]string{"web", "--project", "shop"})
			return c.Execute()
		}, "GET /api/v1/projects/p1/deployments/web/staged"},
		{func() error {
			c := deploymentsDecideCmd(deps, "promote", "", "%s promoted")
			c.SetArgs([]string{"web", "--project", "shop"})
			return c.Execute()
		}, "POST /api/v1/projects/p1/deployments/web/promote"},
		{func() error {
			c := deploymentsDecideCmd(deps, "discard", "", "%s discarded")
			c.SetArgs([]string{"web", "--project", "shop"})
			return c.Execute()
		}, "POST /api/v1/projects/p1/deployments/web/discard"},
	} {
		if err := c.cmd(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(seen, "\n"), c.want) {
			t.Errorf("missing %q in %v", c.want, seen)
		}
	}
}

func TestPromoteWithNothingWaitingSaysSo(t *testing.T) {
	var seen []string
	srv := stagedServer(t, &seen, http.StatusNotFound)
	defer srv.Close()
	c := deploymentsDecideCmd(newTestDeps(srv), "promote", "", "%s")
	c.SetArgs([]string{"web", "--project", "shop"})
	c.SilenceUsage, c.SilenceErrors = true, true
	err := c.Execute()
	if err == nil || !strings.Contains(err.Error(), "promote web") {
		t.Fatalf("got %v", err)
	}
}
