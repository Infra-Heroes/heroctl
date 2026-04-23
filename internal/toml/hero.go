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
	App    AppConfig         `toml:"app"`
	Deploy DeployConfig      `toml:"deploy"`
	Env    map[string]string `toml:"env"`
}

// AppConfig holds the application identity configuration.
type AppConfig struct {
	// Name is the app name used as the public subdomain (e.g. "my-app" → my-app.infra-heroes.de).
	// Must be lowercase and contain only letters, digits, and hyphens.
	Name string `toml:"name"`
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
	if cfg.Deploy.HealthPath == "" {
		return nil, fmt.Errorf("hero.toml: [deploy] health_path is required")
	}
	if cfg.Env == nil {
		cfg.Env = make(map[string]string)
	}

	return &cfg, nil
}
