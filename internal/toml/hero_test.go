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

func TestParse_Scaling(t *testing.T) {
	tomlContent := `
[app]
name = "my-app"

[deploy]
health_path = "/health"
min_replicas = 2
max_replicas = 5
`

	cfg, err := Parse(strings.NewReader(tomlContent))
	if err != nil {
		t.Fatalf("unexpected error parsing hero.toml: %v", err)
	}

	if cfg.Deploy.MinReplicas != 2 {
		t.Errorf("expected MinReplicas to be 2, got %d", cfg.Deploy.MinReplicas)
	}
	if cfg.Deploy.MaxReplicas != 5 {
		t.Errorf("expected MaxReplicas to be 5, got %d", cfg.Deploy.MaxReplicas)
	}

	// Test default scaling values
	tomlContentDefaults := `
[app]
name = "my-app"

[deploy]
health_path = "/health"
`
	cfgDefaults, err := Parse(strings.NewReader(tomlContentDefaults))
	if err != nil {
		t.Fatalf("unexpected error parsing hero.toml: %v", err)
	}
	if cfgDefaults.Deploy.MinReplicas != 1 {
		t.Errorf("expected default MinReplicas to be 1, got %d", cfgDefaults.Deploy.MinReplicas)
	}
	if cfgDefaults.Deploy.MaxReplicas != 1 {
		t.Errorf("expected default MaxReplicas to be 1, got %d", cfgDefaults.Deploy.MaxReplicas)
	}

	// Test max_replicas < min_replicas adjustments
	tomlContentInvalidRange := `
[app]
name = "my-app"

[deploy]
health_path = "/health"
min_replicas = 4
max_replicas = 2
`
	cfgRange, err := Parse(strings.NewReader(tomlContentInvalidRange))
	if err != nil {
		t.Fatalf("unexpected error parsing hero.toml: %v", err)
	}
	if cfgRange.Deploy.MaxReplicas != 4 {
		t.Errorf("expected MaxReplicas to be adjusted to MinReplicas (4), got %d", cfgRange.Deploy.MaxReplicas)
	}
}

func TestParse_VerifyDomainsIsOffUnlessSet(t *testing.T) {
	off, err := Parse(strings.NewReader("[app]\nname = \"a\"\ncustom_domains = [\"shop.example.com\"]\n[deploy]\nport = 80\nhealth_path = \"/\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if off.App.VerifyDomains {
		t.Error("verify_domains must be off unless the file says so: existing hero.toml files keep working")
	}

	on, err := Parse(strings.NewReader("[app]\nname = \"a\"\ncustom_domains = [\"shop.example.com\"]\nverify_domains = true\n[deploy]\nport = 80\nhealth_path = \"/\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !on.App.VerifyDomains {
		t.Error("verify_domains = true was not read")
	}
}

func TestParse_WarnsAboutKeysThatAreIgnored(t *testing.T) {
	cfg, err := Parse(strings.NewReader(`
[app]
name = "x"
port = 80
bogus = 1

[deploy]
health_path = "/"
cpuu = 2
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Deploy.Port != 8080 {
		t.Fatalf("port under [app] must not take effect, got %d", cfg.Deploy.Port)
	}
	got := strings.Join(cfg.Warnings, "\n")
	for _, want := range []string{`"app.port" is ignored: port belongs under [deploy]`, `"app.bogus"`, `"deploy.cpuu"`} {
		if !strings.Contains(got, want) {
			t.Errorf("warnings %q miss %q", got, want)
		}
	}
}

func TestParse_NoWarningsForAValidFile(t *testing.T) {
	cfg, err := Parse(strings.NewReader("[app]\nname = \"x\"\n[deploy]\nport = 80\nhealth_path = \"/\"\n[env]\nA = \"b\"\n[[volumes]]\nname = \"v\"\nmount = \"/d\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", cfg.Warnings)
	}
}

func TestParse_Databases(t *testing.T) {
	cfg, err := Parse(strings.NewReader(`
[app]
name = "shop"
[deploy]
health_path = "/"

[[databases]]
name = "db"
engine = "postgres"
version = 17
size_gb = 10

[[databases]]
name = "cache"
engine = "redis"
version = "7"
env = "CACHE_URL"

[[databases]]
name = "old"
engine = "mysql"
version = 8.4
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Databases) != 3 || len(cfg.Warnings) != 0 {
		t.Fatalf("databases=%v warnings=%v", cfg.Databases, cfg.Warnings)
	}
	for i, want := range []string{"17", "7", "8.4"} {
		if got := cfg.Databases[i].VersionString(); got != want {
			t.Errorf("version %d = %q, want %q", i, got, want)
		}
	}
	if cfg.Databases[1].Env != "CACHE_URL" || cfg.Databases[0].SizeGB != 10 {
		t.Errorf("%+v", cfg.Databases)
	}
}

func TestDatabaseConfig_VersionString(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{{nil, ""}, {"17", "17"}, {int64(17), "17"}, {8.4, "8.4"}, {true, "true"}} {
		if got := (DatabaseConfig{Version: tc.in}).VersionString(); got != tc.want {
			t.Errorf("%v -> %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParse_WorkerNeedsNoPortNoHealthPathAndIsPrivate(t *testing.T) {
	cfg, err := Parse(strings.NewReader(`
[app]
name = "queue"

[deploy]
worker = true
memory_mb = 256
`))
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.Deploy
	if !d.Worker || d.HealthCheckType != "none" || !d.Private || d.HealthPath != "/" {
		t.Errorf("worker = %+v", d)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("worker is a known key: %v", cfg.Warnings)
	}
}

func TestParse_WorkerRefusesWhatAWorkerCannotBe(t *testing.T) {
	for name, extra := range map[string]struct{ app, deploy string }{
		"scale to zero": {"", "scale_to_zero = true"},
		"health check":  {"", `health_check_type = "tcp"`},
		"check port":    {"", "health_check_port = 80"},
		"domain":        {`custom_domains = ["a.example.com"]`, ""},
	} {
		_, err := Parse(strings.NewReader("[app]\nname = \"queue\"\n" + extra.app + "\n[deploy]\nworker = true\n" + extra.deploy + "\n"))
		if err == nil || !strings.Contains(err.Error(), "worker") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestParse_WithoutWorkerHealthPathIsStillRequired(t *testing.T) {
	if _, err := Parse(strings.NewReader("[app]\nname = \"web\"\n[deploy]\nport = 80\n")); err == nil {
		t.Error("an ordinary app must still name its health_path")
	}
}

const jobsApp = "[app]\nname = \"shop\"\n[deploy]\nhealth_path = \"/\"\n"

func TestParse_Jobs(t *testing.T) {
	cfg, err := Parse(strings.NewReader(jobsApp + `
[[jobs]]
name = "cleanup"
schedule = "0 3 * * *"
command = "python cleanup.py"
timezone = "Europe/Berlin"
timeout = "30m"
memory_mb = 1024
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Jobs) != 1 || cfg.Jobs[0].AppName("shop") != "shop-cleanup" || cfg.Jobs[0].MemoryMB != 1024 {
		t.Errorf("jobs = %+v", cfg.Jobs)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("jobs is a known key: %v", cfg.Warnings)
	}
}

func TestParse_JobsRefuseWhatWouldFailLate(t *testing.T) {
	ok := "name = \"a\"\nschedule = \"0 3 * * *\"\ncommand = \"true\"\n"
	for name, jobs := range map[string]string{
		"no name":      "[[jobs]]\nschedule = \"0 3 * * *\"\ncommand = \"true\"\n",
		"bad name":     "[[jobs]]\nname = \"A b\"\nschedule = \"0 3 * * *\"\ncommand = \"true\"\n",
		"twice":        "[[jobs]]\n" + ok + "[[jobs]]\n" + ok,
		"six fields":   "[[jobs]]\nname = \"a\"\nschedule = \"* * * * * *\"\ncommand = \"true\"\n",
		"no schedule":  "[[jobs]]\nname = \"a\"\ncommand = \"true\"\n",
		"no command":   "[[jobs]]\nname = \"a\"\nschedule = \"0 3 * * *\"\n",
		"timeout junk": "[[jobs]]\n" + ok + "timeout = \"soon\"\n",
		"timeout big":  "[[jobs]]\n" + ok + "timeout = \"48h\"\n",
		"long name":    "[[jobs]]\nname = \"" + strings.Repeat("a", 32) + "\"\nschedule = \"0 3 * * *\"\ncommand = \"true\"\n",
	} {
		app := jobsApp
		if name == "long name" {
			app = "[app]\nname = \"" + strings.Repeat("b", 40) + "\"\n[deploy]\nhealth_path = \"/\"\n"
		}
		if _, err := Parse(strings.NewReader(app + jobs)); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}
