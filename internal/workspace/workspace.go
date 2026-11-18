// Package workspace keeps named contexts for heroctl: which server, which login
// and which project a command runs against.
//
// Somebody with a private organisation and a customer's, or with staging and
// production projects, used to log out and in again or type --project on every
// command. A workspace holds that choice; "heroctl workspace use" switches it.
//
// The login that existed before workspaces is the workspace "default": its
// token stays at ~/.config/heroctl/token.json, so nothing changes for anyone who
// never creates a second one.
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Default is the workspace every installation starts with.
const Default = "default"

var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

// Workspace is one named context.
type Workspace struct {
	// Server is the hero-api URL; empty means the one heroctl was built for.
	Server string `json:"server,omitempty"`
	// Project is used when a command is not given --project.
	Project string `json:"project,omitempty"`
}

// File is what workspaces.json holds.
type File struct {
	Current    string               `json:"current,omitempty"`
	Workspaces map[string]Workspace `json:"workspaces"`
}

// Dir is the heroctl configuration directory. A variable so tests can move it.
var Dir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "heroctl"), nil
}

func filePath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "workspaces.json"), nil
}

// ValidName says whether name can be a workspace.
func ValidName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("workspace name %q is invalid: lowercase letters, digits and hyphens, up to 32 characters", name)
	}
	return nil
}

// Load reads workspaces.json; a missing file is the default workspace alone.
func Load() (*File, error) {
	p, err := filePath()
	if err != nil {
		return nil, err
	}
	f := &File{Workspaces: map[string]Workspace{}}
	data, err := os.ReadFile(p) // #nosec G304 -- path derived from the user's home
	if errors.Is(err, os.ErrNotExist) {
		f.Workspaces[Default] = Workspace{}
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	if err := json.Unmarshal(data, f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if f.Workspaces == nil {
		f.Workspaces = map[string]Workspace{}
	}
	if _, ok := f.Workspaces[Default]; !ok {
		f.Workspaces[Default] = Workspace{}
	}
	return f, nil
}

// Save writes workspaces.json.
func (f *File) Save() error {
	p, err := filePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// Names returns the workspaces, sorted.
func (f *File) Names() []string {
	out := make([]string, 0, len(f.Workspaces))
	for n := range f.Workspaces {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Active is the workspace to use: the explicit choice (flag or environment)
// first, then the one set with "use", then the default.
func (f *File) Active(explicit string) (string, Workspace, error) {
	name := explicit
	if name == "" {
		name = f.Current
	}
	if name == "" {
		name = Default
	}
	ws, ok := f.Workspaces[name]
	if !ok {
		return "", Workspace{}, fmt.Errorf("workspace %q does not exist (heroctl workspace list)", name)
	}
	return name, ws, nil
}

// TokenFile is where a workspace keeps its login. The default workspace keeps
// the path it always had.
func TokenFile(name string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	if name == "" || name == Default {
		return filepath.Join(d, "token.json"), nil
	}
	return filepath.Join(d, "tokens", name+".json"), nil
}
