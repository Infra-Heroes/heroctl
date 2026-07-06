package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/auth"
	"github.com/Infra-Heroes/heroctl/internal/client"
)

// newTestDeps returns Deps whose client talks to the given test server.
func newTestDeps(srv *httptest.Server) *Deps {
	tok := &auth.Token{AccessToken: "test-token", Expiry: time.Now().Add(time.Hour)}
	return &Deps{
		Token:  tok,
		Client: client.New(srv.URL, "auth.test", "client-id", tok),
	}
}

func TestOrgsSetLimits(t *testing.T) {
	t.Run("sends only changed flags and prints result", func(t *testing.T) {
		var gotBody map[string]any
		var gotPath, gotMethod string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"ID": "org-1", "Name": "infraheroes",
				"MaxProjects": 5, "VmCap": 2, "MaxCpu": 1, "MaxMemoryMb": 2048,
			})
		}))
		defer srv.Close()

		cmd := orgsSetLimitsCmd(newTestDeps(srv))
		cmd.SetArgs([]string{"--org", "org-1", "--max-projects", "5"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}

		if gotMethod != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", gotMethod)
		}
		if gotPath != "/api/v1/orgs/org-1/limits" {
			t.Errorf("path = %s, want /api/v1/orgs/org-1/limits", gotPath)
		}
		if v, ok := gotBody["max_projects"].(float64); !ok || v != 5 {
			t.Errorf("body max_projects = %v, want 5", gotBody["max_projects"])
		}
		for _, absent := range []string{"vm_cap", "max_cpu", "max_memory_mb"} {
			if _, present := gotBody[absent]; present {
				t.Errorf("body must not contain %s when its flag is not set", absent)
			}
		}
	})

	t.Run("no flags returns error without calling the API", func(t *testing.T) {
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))
		defer srv.Close()

		cmd := orgsSetLimitsCmd(newTestDeps(srv))
		cmd.SetArgs([]string{"--org", "org-1"})
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error when no limit flags are set")
		}
		if called {
			t.Error("API must not be called when validation fails")
		}
	})

	t.Run("defaults to own org when --org omitted", func(t *testing.T) {
		var paths []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.Method+" "+r.URL.Path)
			switch r.URL.Path {
			case "/api/v1/orgs/me":
				json.NewEncoder(w).Encode(map[string]any{"ID": "own-org-id", "Name": "infraheroes"})
			default:
				json.NewEncoder(w).Encode(map[string]any{
					"ID": "own-org-id", "Name": "infraheroes",
					"MaxProjects": 9, "VmCap": 2, "MaxCpu": 1, "MaxMemoryMb": 2048,
				})
			}
		}))
		defer srv.Close()

		cmd := orgsSetLimitsCmd(newTestDeps(srv))
		cmd.SetArgs([]string{"--max-projects", "9"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}

		want := []string{"GET /api/v1/orgs/me", "PATCH /api/v1/orgs/own-org-id/limits"}
		if len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
			t.Errorf("requests = %v, want %v", paths, want)
		}
	})

	t.Run("surfaces API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "only platform admins can update org limits"})
		}))
		defer srv.Close()

		cmd := orgsSetLimitsCmd(newTestDeps(srv))
		cmd.SetArgs([]string{"--org", "org-1", "--vm-cap", "10"})
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error from 403 response")
		}
	})
}
