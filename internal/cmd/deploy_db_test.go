package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type platform struct {
	mu          sync.Mutex
	deployments []map[string]any // bodies posted
	secrets     []string
	volumes     []string
}

func newPlatform(t *testing.T, p *platform) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "org"})
		case "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case "GET /api/v1/projects/proj-1/deployments":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case "GET /api/v1/projects/proj-1/volumes":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case "POST /api/v1/projects/proj-1/volumes":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			p.volumes = append(p.volumes, b["name"].(string))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "vol-" + b["name"].(string), "name": b["name"]})
		case "POST /api/v1/projects/proj-1/secrets":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			p.secrets = append(p.secrets, b["key"].(string))
		case "POST /api/v1/projects/proj-1/deployments":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			p.deployments = append(p.deployments, b)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"app_name": b["app_name"], "hostname": "x.example"})
		default:
			if strings.HasSuffix(r.URL.Path, "/status") {
				_ = json.NewEncoder(w).Encode(map[string]any{"nomad_status": "running", "healthy": true})
				return
			}
			http.NotFound(w, r)
		}
	}))
}

func writeToml(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hero.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("PATH", t.TempDir()) // no container engine: --image must be enough
}

const shopToml = `[app]
name = "shop"

[deploy]
port = 8080
health_path = "/"

[[databases]]
name = "db"
engine = "postgres"
size_gb = 2

[[databases]]
name = "cache"
engine = "redis"
`

func TestDeployCreatesTheDeclaredDatabasesAndHandsTheAppTheirURLs(t *testing.T) {
	writeToml(t, shopToml)
	p := &platform{}
	srv := newPlatform(t, p)
	defer srv.Close()

	if err := execCmd(t, deployCmd(newTestDeps(srv)), "--project", "app", "--image", "docker.io/library/nginx:1.31", "--yes"); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, d := range p.deployments {
		names = append(names, d["app_name"].(string))
	}
	// Databases first, the app last: it must find them when it starts.
	if strings.Join(names, ",") != "shop-db,shop-cache,shop" {
		t.Fatalf("deployment order = %v", names)
	}
	if strings.Join(p.volumes, ",") != "shop-db-data,shop-cache-data" {
		t.Errorf("volumes = %v", p.volumes)
	}
	app := p.deployments[2]["env"].(map[string]any)
	if app["DATABASE_URL"] != "secret:SHOP_DB_URL" || app["REDIS_URL"] != "secret:SHOP_CACHE_URL" {
		t.Errorf("the app's env = %v", app)
	}
	db := p.deployments[0]
	if db["service_scope"] != "internal" || db["health_check_type"] != "tcp" {
		t.Errorf("a database must be private and TCP-checked: %v", db)
	}
	if pw := db["env"].(map[string]any)["POSTGRES_PASSWORD"]; pw != "secret:SHOP_DB_PASSWORD" {
		t.Errorf("the password must reach the database as a secret reference, got %v", pw)
	}
}

func TestDeployRefusesAnEnvVariableThatAlreadyHoldsAURL(t *testing.T) {
	writeToml(t, shopToml+"\n[env]\nDATABASE_URL = \"postgres://mine\"\n")
	p := &platform{}
	srv := newPlatform(t, p)
	defer srv.Close()

	err := execCmd(t, deployCmd(newTestDeps(srv)), "--project", "app", "--image", "docker.io/library/nginx:1.31", "--yes")
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("err = %v", err)
	}
	if len(p.deployments) != 0 || len(p.secrets) != 0 || len(p.volumes) != 0 {
		t.Errorf("nothing may be created when the file is inconsistent: %+v", p)
	}
}

func TestDeployRefusesAnUnknownEngineBeforeCreatingAnything(t *testing.T) {
	writeToml(t, "[app]\nname = \"shop\"\n[deploy]\nhealth_path = \"/\"\n[[databases]]\nname = \"db\"\nengine = \"oracle\"\n")
	p := &platform{}
	srv := newPlatform(t, p)
	defer srv.Close()

	err := execCmd(t, deployCmd(newTestDeps(srv)), "--project", "app", "--image", "docker.io/library/nginx:1.31", "--yes")
	if err == nil || !strings.Contains(err.Error(), "oracle") {
		t.Fatalf("err = %v", err)
	}
	if len(p.deployments) != 0 {
		t.Error("an unknown engine must stop the deploy before anything exists")
	}
}
