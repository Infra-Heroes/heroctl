package toml

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tomlContent := `
[app]
name = "my-app"

[deploy]
cpu = 2
memory_mb = 1024
port = 3000
health_path = "/health"
scale_to_zero = true
private = false
health_check_type = "http"
health_check_port = 3000

[env]
DATABASE_URL = "postgres://..."

[labels]
"metrics.scrape" = "true"
"metrics.path" = "/metrics"
`

	cfg, err := Parse(strings.NewReader(tomlContent))
	if err != nil {
		t.Fatalf("unexpected error parsing hero.toml: %v", err)
	}

	if cfg.App.Name != "my-app" {
		t.Errorf("expected App.Name to be 'my-app', got %q", cfg.App.Name)
	}
	if cfg.Deploy.CPU != 2 {
		t.Errorf("expected Deploy.CPU to be 2, got %d", cfg.Deploy.CPU)
	}
	if cfg.Env["DATABASE_URL"] != "postgres://..." {
		t.Errorf("expected Env DATABASE_URL, got %q", cfg.Env["DATABASE_URL"])
	}
	if cfg.Labels["metrics.scrape"] != "true" {
		t.Errorf("expected Label metrics.scrape to be 'true', got %q", cfg.Labels["metrics.scrape"])
	}
	if cfg.Labels["metrics.path"] != "/metrics" {
		t.Errorf("expected Label metrics.path to be '/metrics', got %q", cfg.Labels["metrics.path"])
	}
}

func TestParse_MissingLabels(t *testing.T) {
	tomlContent := `
[app]
name = "my-app"

[deploy]
health_path = "/health"
`

	cfg, err := Parse(strings.NewReader(tomlContent))
	if err != nil {
		t.Fatalf("unexpected error parsing hero.toml: %v", err)
	}

	if cfg.Labels == nil {
		t.Fatalf("expected Labels map to be initialized, got nil")
	}
	if len(cfg.Labels) != 0 {
		t.Errorf("expected Labels map to be empty, got size %d", len(cfg.Labels))
	}
}
