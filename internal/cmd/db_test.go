package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type dbServer struct{ calls []string }

func newDBServer(t *testing.T, s *dbServer) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		s.calls = append(s.calls, key)
		switch {
		case key == "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case key == "GET /api/v1/projects/proj-1/deployments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"app_name": "shop", "status": "running", "image": "api/x/app:1"},
				{"app_name": "shop-db", "status": "running", "image": "docker.io/library/postgres:17-alpine",
					"labels": map[string]string{"hero-managed-db": "postgres", "hero-db-for": "shop"}},
			})
		case key == "GET /api/v1/projects/proj-1/volumes":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "vol-9", "name": "shop-db-data", "size_gb": 5, "status": "available"}})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
}

func runDB(t *testing.T, srv *httptest.Server, stdin string, args ...string) error {
	t.Helper()
	cmd := dbCmd(newTestDeps(srv))
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd.Execute()
}

func deleted(calls []string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c, "DELETE ") {
			out = append(out, c)
		}
	}
	return out
}

func TestDBList_ShowsOnlyManagedDatabases(t *testing.T) {
	s := &dbServer{}
	srv := newDBServer(t, s)
	defer srv.Close()
	if err := runDB(t, srv, "", "list", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	if len(deleted(s.calls)) != 0 {
		t.Error("listing must delete nothing")
	}
}

func TestDBDestroy_RemovesDeploymentVolumeAndBothSecrets(t *testing.T) {
	s := &dbServer{}
	srv := newDBServer(t, s)
	defer srv.Close()
	if err := runDB(t, srv, "", "destroy", "shop-db", "--project", "app", "--force"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(deleted(s.calls), "\n")
	for _, want := range []string{
		"DELETE /api/v1/projects/proj-1/deployments/shop-db",
		"DELETE /api/v1/projects/proj-1/volumes/vol-9",
		"DELETE /api/v1/projects/proj-1/secrets/SHOP_DB_PASSWORD",
		"DELETE /api/v1/projects/proj-1/secrets/SHOP_DB_URL",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestDBDestroy_RefusesWhatIsNotAManagedDatabase(t *testing.T) {
	s := &dbServer{}
	srv := newDBServer(t, s)
	defer srv.Close()
	err := runDB(t, srv, "", "destroy", "shop", "--project", "app", "--force")
	if err == nil || !strings.Contains(err.Error(), "not a managed database") {
		t.Fatalf("err = %v", err)
	}
	if len(deleted(s.calls)) != 0 {
		t.Errorf("an ordinary app must never be deleted by db destroy: %v", deleted(s.calls))
	}
}

func TestDBDestroy_DecliningDeletesNothing(t *testing.T) {
	s := &dbServer{}
	srv := newDBServer(t, s)
	defer srv.Close()
	if err := runDB(t, srv, "n\n", "destroy", "shop-db", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	if len(deleted(s.calls)) != 0 {
		t.Errorf("declined, yet deleted %v", deleted(s.calls))
	}
}
