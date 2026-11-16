// Package toml parses hero.toml project deployment configuration files.
package toml

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var validAppName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// HeroConfig is the parsed representation of a hero.toml file.
type HeroConfig struct {
	App     AppConfig         `toml:"app"`
	Deploy  DeployConfig      `toml:"deploy"`
	Env     map[string]string `toml:"env"`
	Labels  map[string]string `toml:"labels"`
	Volumes []VolumeConfig    `toml:"volumes"`
	// Databases are provisioned by heroctl as private deployments of their own.
	Databases []DatabaseConfig `toml:"databases"`

	// Warnings lists keys that were set but are not known. They are not an error
	// (an older heroctl must still read a newer file), but a key that is silently
	// ignored, such as a port written under [app], leaves the app on its default
	// and the deploy waiting for a health check that can never pass.
	Warnings []string `toml:"-"`
}

// DatabaseConfig asks for a managed database next to the app.
type DatabaseConfig struct {
	// Name identifies the database within the app ("db", "cache").
	Name string `toml:"name"`
	// Engine is postgres, mysql or redis.
	Engine string `toml:"engine"`
	// Version is the engine's major version. A bare number (17, 8.4) is accepted
	// as well as a string.
	Version any `toml:"version"`
	// SizeGB is the size of the data volume (default 5).
	SizeGB int `toml:"size_gb"`
	// Env is the variable the connection URL is handed to the app under; the
	// default is DATABASE_URL (postgres, mysql) or REDIS_URL.
	Env string `toml:"env"`
}

// VersionString returns the version as written, however it was typed.
func (d DatabaseConfig) VersionString() string {
	switch v := d.Version.(type) {
	case nil:
		return ""
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(d.Version)
}

// VolumeConfig declares a volume attachment for a deployment.
type VolumeConfig struct {
	// Name is the volume's name within this project. Must already exist (created via heroctl volumes create).
	Name string `toml:"name"`
	// Mount is the absolute path inside the VM where the volume will be mounted.
	Mount string `toml:"mount"`
}

// AppConfig holds the application identity configuration.
type AppConfig struct {
	// Name is the app name used as the public subdomain (e.g. "my-app" → my-app.infra-heroes.de).
	// Must be lowercase and contain only letters, digits, and hyphens.
	Name          string   `toml:"name"`
	CustomDomains []string `toml:"custom_domains"`
	// VerifyDomains asks the platform to check that the organisation controls each
	// custom domain, by a TXT record, before deploying. Off unless set: without it
	// custom domains are routed as they always were.
	VerifyDomains bool `toml:"verify_domains"`
}

// DeployConfig holds deployment resource configuration.
type DeployConfig struct {
	// CPU is the number of vCPUs (default 1).
	CPU int `toml:"cpu"`
	// MemoryMB is the memory allocation in megabytes (default 512).
	MemoryMB int `toml:"memory_mb"`
	// Port is the guest port the application listens on (default 8080).
	Port int `toml:"port"`
	// HealthPath is the HTTP path used for the Nomad health check (default "/").
	HealthPath string `toml:"health_path"`
	// ScaleToZero enables automatic shutdown of the app when idle (default false).
	ScaleToZero bool `toml:"scale_to_zero"`
	// Worker marks a background process: it listens on nothing, has no address and no
	// health check, and runs until it is stopped. port, health_path and the other
	// health settings do not apply to it, and it cannot be public or scale to zero.
	Worker bool `toml:"worker"`
	// Private marks the deployment as internal-only (no public relay, no Traefik route).
	// Default false (public).
	Private bool `toml:"private"`
	// HealthCheckType is the Nomad health check protocol: "http" or "tcp".
	// Empty string defaults to "http".
	HealthCheckType string `toml:"health_check_type"`
	// HealthCheckPort is the port used for health checks.
	// 0 means use the Port field.
	HealthCheckPort int `toml:"health_check_port"`
	// MinReplicas is the minimum number of replicas (default 1).
	MinReplicas int `toml:"min_replicas"`
	// MaxReplicas is the maximum number of replicas (default 1).
	MaxReplicas int `toml:"max_replicas"`
}

// Parse reads and validates a hero.toml from r.
func Parse(r io.Reader) (*HeroConfig, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read hero.toml: %w", err)
	}
	var cfg HeroConfig
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse hero.toml: %w", err)
	}
	cfg.Warnings = unknownKeys(raw)

	if cfg.App.Name == "" {
		return nil, fmt.Errorf("hero.toml: [app] name is required")
	}
	if !validAppName.MatchString(cfg.App.Name) {
		return nil, fmt.Errorf("hero.toml: [app] name must be lowercase alphanumeric with hyphens (no leading/trailing hyphens)")
	}

	// Apply defaults.
	if cfg.Deploy.CPU <= 0 {
		cfg.Deploy.CPU = 1
	}
	if cfg.Deploy.MemoryMB <= 0 {
		cfg.Deploy.MemoryMB = 512
	}
	if cfg.Deploy.Port <= 0 {
		cfg.Deploy.Port = 8080
	}
	if cfg.Deploy.MinReplicas <= 0 {
		cfg.Deploy.MinReplicas = 1
	}
	if cfg.Deploy.MaxReplicas <= 0 {
		cfg.Deploy.MaxReplicas = 1
	}
	if cfg.Deploy.MaxReplicas < cfg.Deploy.MinReplicas {
		cfg.Deploy.MaxReplicas = cfg.Deploy.MinReplicas
	}
	if cfg.Deploy.Worker {
		if err := cfg.makeWorker(); err != nil {
			return nil, err
		}
	}
	if cfg.Deploy.HealthPath == "" {
		return nil, fmt.Errorf("hero.toml: [deploy] health_path is required")
	}
	if cfg.Env == nil {
		cfg.Env = make(map[string]string)
	}
	if cfg.Labels == nil {
		cfg.Labels = make(map[string]string)
	}

	// Validate health_check_type.
	switch cfg.Deploy.HealthCheckType {
	case "", "http", "tcp":
		// valid
	case "none":
		// Only a worker has none, and makeWorker is what gives it.
		if !cfg.Deploy.Worker {
			return nil, fmt.Errorf("hero.toml: [deploy] health_check_type \"none\" is for workers: set worker = true instead")
		}
	default:
		return nil, fmt.Errorf("hero.toml: [deploy] health_check_type must be \"http\" or \"tcp\" (got %q)", cfg.Deploy.HealthCheckType)
	}
	// Validate health_check_port.
	if cfg.Deploy.HealthCheckPort < 0 {
		return nil, fmt.Errorf("hero.toml: [deploy] health_check_port must be >= 0 (got %d)", cfg.Deploy.HealthCheckPort)
	}

	// Validate volume blocks.
	seenMounts := make(map[string]bool)
	for i, v := range cfg.Volumes {
		if v.Name == "" {
			return nil, fmt.Errorf("hero.toml: [[volumes]][%d] name is required", i)
		}
		if v.Mount == "" {
			return nil, fmt.Errorf("hero.toml: [[volumes]][%d] mount is required", i)
		}
		if v.Mount[0] != '/' {
			return nil, fmt.Errorf("hero.toml: [[volumes]][%d] mount must be an absolute path (start with /)", i)
		}
		if seenMounts[v.Mount] {
			return nil, fmt.Errorf("hero.toml: [[volumes]] duplicate mount path %q", v.Mount)
		}
		seenMounts[v.Mount] = true
	}

	return &cfg, nil
}

// makeWorker settles a [deploy] worker = true: it refuses what a worker cannot be
// and fills in what the platform needs to store it.
func (cfg *HeroConfig) makeWorker() error {
	d := &cfg.Deploy
	switch {
	case d.ScaleToZero:
		return fmt.Errorf("hero.toml: [deploy] a worker cannot scale_to_zero: nothing would ever start it again")
	case len(cfg.App.CustomDomains) > 0:
		return fmt.Errorf("hero.toml: [app] a worker has no address, so custom_domains does not apply")
	case d.HealthCheckType != "" || d.HealthCheckPort != 0:
		return fmt.Errorf("hero.toml: [deploy] a worker has no health check: remove health_check_type and health_check_port")
	}
	d.HealthCheckType = "none"
	d.Private = true
	d.HealthPath = "/"
	return nil
}

// deployKeys are the keys that belong under [deploy]; written elsewhere they are
// a common slip, and the warning can then say where they go.
var deployKeys = map[string]bool{
	"cpu": true, "memory_mb": true, "port": true, "health_path": true, "scale_to_zero": true,
	"private": true, "health_check_type": true, "health_check_port": true,
	"min_replicas": true, "max_replicas": true, "worker": true,
}

// unknownKeys reports the keys of raw that HeroConfig does not read.
func unknownKeys(raw []byte) []string {
	var probe HeroConfig
	d := toml.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	var strict *toml.StrictMissingError
	if err := d.Decode(&probe); !errors.As(err, &strict) {
		return nil
	}
	var out []string
	for _, e := range strict.Errors {
		key := e.Key()
		name := strings.Join(key, ".")
		last := key[len(key)-1]
		if len(key) == 2 && key[0] == "app" && deployKeys[last] {
			out = append(out, fmt.Sprintf("%q is ignored: %s belongs under [deploy], not [app]", name, last))
			continue
		}
		if len(key) == 1 && deployKeys[last] {
			out = append(out, fmt.Sprintf("%q is ignored: %s belongs under [deploy]", name, last))
			continue
		}
		out = append(out, fmt.Sprintf("%q is not a known hero.toml key and is ignored", name))
	}
	return out
}
