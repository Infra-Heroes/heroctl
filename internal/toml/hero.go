// Package toml parses hero.toml project deployment configuration files.
package toml

import (
	"fmt"
	"io"
	"regexp"

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
	var cfg HeroConfig
	if err := toml.NewDecoder(r).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse hero.toml: %w", err)
	}

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
