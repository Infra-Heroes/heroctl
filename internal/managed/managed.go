// Package managed provisions the databases a hero.toml asks for.
//
// A microVM runs one image, so a database is not a sidecar inside the app's VM:
// it is a deployment of its own in the same project, private (no public route),
// with a volume for its data and a generated password kept as a secret. The app
// reaches it by the short service name, which the project's DNS resolves, and is
// handed a ready connection URL as an environment variable.
//
// Provisioning is idempotent. A database that exists is left alone (and started
// if it was stopped); nothing is redeployed on every app deploy, because that
// would restart the database each time.
package managed

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// Labels mark a deployment as a managed database, so that `heroctl db` can find
// them and nothing else.
const (
	LabelEngine = "hero-managed-db"
	LabelFor    = "hero-db-for"
)

// Engine describes one kind of database: the public image, how it is reached
// and what it needs to keep its data.
type Engine struct {
	Name           string
	DefaultVersion string
	// Image is the public image for a version.
	Image func(version string) string
	Port  int
	// Mount is where the volume goes. Empty means the engine keeps nothing.
	Mount string
	// Env is the container environment. password is a "secret:KEY" reference.
	Env func(password string) map[string]string
	// URL builds the connection URL.
	URL func(host, password string) string
	// EnvVar is the name the URL is handed to the app under by default.
	EnvVar   string
	CPU      int
	MemoryMB int
}

const dbUser, dbName = "app", "app"

// Engines are the supported engines.
var Engines = map[string]Engine{
	"postgres": {
		Name:           "postgres",
		DefaultVersion: "17",
		Image:          func(v string) string { return "docker.io/library/postgres:" + v + "-alpine" },
		Port:           5432,
		// The parent of the data directory: a volume's root holds lost+found, which
		// initdb refuses, and PGDATA lives one level below.
		Mount: "/var/lib/postgresql",
		Env: func(pw string) map[string]string {
			return map[string]string{"POSTGRES_USER": dbUser, "POSTGRES_DB": dbName, "POSTGRES_PASSWORD": pw}
		},
		URL: func(host, pw string) string {
			return fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", dbUser, pw, host, dbName)
		},
		EnvVar:   "DATABASE_URL",
		CPU:      1,
		MemoryMB: 512,
	},
	// The official mysql image cannot start in a microVM: it is Oracle Linux
	// without a mount binary, so the guest init never gets /proc and the server
	// stops at its first stat of /proc/self/exe. MariaDB speaks the same protocol
	// and URLs, and its image is Debian based.
	"mariadb": {
		Name:           "mariadb",
		DefaultVersion: "11.4",
		Image:          func(v string) string { return "docker.io/library/mariadb:" + v },
		Port:           3306,
		Mount:          "/var/lib/mysql",
		Env: func(pw string) map[string]string {
			return map[string]string{
				"MARIADB_USER": dbUser, "MARIADB_DATABASE": dbName, "MARIADB_PASSWORD": pw,
				"MARIADB_RANDOM_ROOT_PASSWORD": "1",
			}
		},
		URL: func(host, pw string) string {
			return fmt.Sprintf("mysql://%s:%s@%s:3306/%s", dbUser, pw, host, dbName)
		},
		EnvVar:   "DATABASE_URL",
		CPU:      1,
		MemoryMB: 1024,
	},
	"redis": {
		Name:           "redis",
		DefaultVersion: "8",
		Image:          func(v string) string { return "docker.io/library/redis:" + v + "-alpine" },
		Port:           6379,
		Mount:          "/data",
		// The official image takes no password from the environment. Redis is
		// reachable only inside the project's isolated network.
		Env:      func(string) map[string]string { return nil },
		URL:      func(host, _ string) string { return fmt.Sprintf("redis://%s:6379", host) },
		EnvVar:   "REDIS_URL",
		CPU:      1,
		MemoryMB: 256,
	},
}

// Spec is one [[databases]] block of hero.toml.
type Spec struct {
	Name    string
	Engine  string
	Version string
	SizeGB  int
	// Env overrides the variable the URL is handed to the app under.
	Env string
}

var (
	nameRe    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	versionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)
	envRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Names are what one database is called in the platform.
type Names struct {
	Deployment, Volume, PasswordKey, URLKey string
}

// NamesFor returns the names for database name of app.
func NamesFor(app, name string) Names {
	dep := app + "-" + name
	key := strings.ToUpper(strings.ReplaceAll(dep, "-", "_"))
	return Names{Deployment: dep, Volume: dep + "-data", PasswordKey: key + "_PASSWORD", URLKey: key + "_URL"}
}

// Validate checks the specs of app and fills defaults.
func Validate(app string, specs []Spec) ([]Spec, error) {
	out := make([]Spec, len(specs))
	seen := map[string]bool{}
	vars := map[string]string{}
	for i, s := range specs {
		if !nameRe.MatchString(s.Name) {
			return nil, fmt.Errorf("[[databases]] #%d: name %q must be lowercase letters, digits and hyphens", i+1, s.Name)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("[[databases]]: name %q is used twice", s.Name)
		}
		seen[s.Name] = true
		if s.Engine == "mysql" {
			s.Engine = "mariadb" // MySQL-compatible; see Engines
		}
		eng, ok := Engines[s.Engine]
		if !ok {
			return nil, fmt.Errorf("[[databases]] %q: engine %q is not supported (use postgres, mariadb (MySQL compatible) or redis)", s.Name, s.Engine)
		}
		if s.Version == "" {
			s.Version = eng.DefaultVersion
		}
		if !versionRe.MatchString(s.Version) {
			return nil, fmt.Errorf("[[databases]] %q: version %q must be a number such as %s", s.Name, s.Version, eng.DefaultVersion)
		}
		if s.SizeGB == 0 {
			s.SizeGB = 5
		}
		if s.SizeGB < 1 || s.SizeGB > 1000 {
			return nil, fmt.Errorf("[[databases]] %q: size_gb must be between 1 and 1000", s.Name)
		}
		if n := NamesFor(app, s.Name).Deployment; len(n) > 63 {
			return nil, fmt.Errorf("[[databases]] %q: %q is longer than 63 characters; use a shorter name", s.Name, n)
		}
		if s.Env == "" {
			s.Env = eng.EnvVar
		} else if !envRe.MatchString(s.Env) {
			return nil, fmt.Errorf("[[databases]] %q: env %q is not a valid variable name", s.Name, s.Env)
		}
		if other, dup := vars[s.Env]; dup {
			return nil, fmt.Errorf("[[databases]] %q and %q both set %s; give one of them another env name", other, s.Name, s.Env)
		}
		vars[s.Env] = s.Name
		out[i] = s
	}
	return out, nil
}

// API is the part of the platform client that provisioning uses.
type API interface {
	ListDeployments(ctx context.Context, projectID string) ([]client.Deployment, error)
	CreateDeployment(ctx context.Context, projectID string, req client.CreateDeploymentRequest) (*client.DeploymentCreated, error)
	GetDeploymentStatus(ctx context.Context, projectID, app string) (*client.DeploymentStatus, error)
	StartDeployment(ctx context.Context, projectID, app string) (*client.DeploymentCreated, error)
	ListVolumes(ctx context.Context, projectID string) ([]client.Volume, error)
	CreateVolume(ctx context.Context, projectID string, req client.CreateVolumeRequest) (*client.Volume, error)
	SetSecret(ctx context.Context, projectID, key, value string) error
	DeleteDeployment(ctx context.Context, projectID, app string) error
	DeleteVolume(ctx context.Context, projectID, volumeID string) error
	DeleteSecret(ctx context.Context, projectID, key string) error
}

// Result of provisioning one database.
type Result struct {
	Spec    Spec
	Names   Names
	Created bool
	// EnvVar and EnvValue are what the app is given: a reference to the URL secret.
	EnvVar, EnvValue string
}

// Options tune Provision; the zero value is right for production.
type Options struct {
	Poll    time.Duration
	Timeout time.Duration
	Out     func(format string, a ...any)
	// Password supplies passwords; nil generates a random one.
	Password func() (string, error)
}

// Provision makes sure every database of app exists and is healthy, and returns
// what to hand the app.
func Provision(ctx context.Context, api API, projectID, app string, specs []Spec, opt Options) ([]Result, error) {
	if opt.Poll == 0 {
		opt.Poll = 2 * time.Second
	}
	if opt.Timeout == 0 {
		opt.Timeout = 4 * time.Minute
	}
	if opt.Out == nil {
		opt.Out = func(string, ...any) {}
	}
	if opt.Password == nil {
		opt.Password = GeneratePassword
	}
	deps, err := api.ListDeployments(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	existing := map[string]client.Deployment{}
	for _, d := range deps {
		existing[d.AppName] = d
	}
	vols, err := api.ListVolumes(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	volByName := map[string]client.Volume{}
	for _, v := range vols {
		volByName[v.Name] = v
	}

	var results []Result
	for _, s := range specs {
		eng := Engines[s.Engine]
		n := NamesFor(app, s.Name)
		res := Result{Spec: s, Names: n, EnvVar: s.Env, EnvValue: "secret:" + n.URLKey}

		if d, ok := existing[n.Deployment]; ok {
			if d.Labels[LabelEngine] == "" {
				return nil, fmt.Errorf("database %q: a deployment called %q exists in this project and is not a managed database", s.Name, n.Deployment)
			}
			if d.Status == "stopped" {
				opt.Out("Database %s is stopped, starting it.\n", n.Deployment)
				if _, err := api.StartDeployment(ctx, projectID, n.Deployment); err != nil {
					return nil, fmt.Errorf("start database %s: %w", n.Deployment, err)
				}
				if err := waitHealthy(ctx, api, projectID, n.Deployment, opt); err != nil {
					return nil, err
				}
			} else {
				opt.Out("Database %s exists.\n", n.Deployment)
			}
			results = append(results, res)
			continue
		}
		if _, ok := volByName[n.Volume]; ok {
			return nil, fmt.Errorf("database %q: the volume %q exists without its deployment. Its data was written with a password that is no longer known. "+
				"Delete the volume (heroctl volumes destroy %s) to start fresh, or recreate the deployment yourself", s.Name, n.Volume, n.Volume)
		}

		opt.Out("Creating %s database %s (%s, %d GB)...\n", s.Engine, n.Deployment, eng.Image(s.Version), s.SizeGB)
		if err := create(ctx, api, projectID, app, s, eng, n, opt); err != nil {
			return nil, err
		}
		opt.Out("Database %s is ready.\n", n.Deployment)
		res.Created = true
		results = append(results, res)
	}
	return results, nil
}

// create makes the secrets, the volume and the deployment of one database, and
// takes back whatever it made if a step fails. Left behind, a half-made database
// would block the next deploy: its volume holds data under a password nobody
// kept, and the name is taken.
func create(ctx context.Context, api API, projectID, app string, s Spec, eng Engine, n Names, opt Options) (err error) {
	var made struct {
		secrets    []string
		volumeID   string
		deployment bool
	}
	defer func() {
		if err == nil {
			return
		}
		opt.Out("Removing the half-made database %s.\n", n.Deployment)
		// The caller's context may be what failed; clean up regardless.
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		if made.deployment {
			_ = api.DeleteDeployment(bg, projectID, n.Deployment)
		}
		if made.volumeID != "" {
			_ = api.DeleteVolume(bg, projectID, made.volumeID)
		}
		for _, k := range made.secrets {
			_ = api.DeleteSecret(bg, projectID, k)
		}
	}()

	pw, err := opt.Password()
	if err != nil {
		return err
	}
	for _, kv := range [][2]string{{n.PasswordKey, pw}, {n.URLKey, eng.URL(n.Deployment, pw)}} {
		if err := api.SetSecret(ctx, projectID, kv[0], kv[1]); err != nil {
			return secretError(err)
		}
		made.secrets = append(made.secrets, kv[0])
	}
	vol, err := api.CreateVolume(ctx, projectID, client.CreateVolumeRequest{Name: n.Volume, SizeGB: s.SizeGB})
	if err != nil {
		return fmt.Errorf("create volume %s: %w", n.Volume, err)
	}
	made.volumeID = vol.ID
	if _, err := api.CreateDeployment(ctx, projectID, client.CreateDeploymentRequest{
		AppName:         n.Deployment,
		Image:           eng.Image(s.Version),
		CPU:             eng.CPU,
		MemoryMB:        eng.MemoryMB,
		Port:            eng.Port,
		Env:             eng.Env("secret:" + n.PasswordKey),
		Labels:          map[string]string{LabelEngine: s.Engine, LabelFor: app},
		HealthPath:      "/",
		ServiceScope:    "internal",
		HealthCheckType: "tcp",
		Volumes:         []client.VolumeAttachment{{VolumeID: vol.ID, MountPath: eng.Mount}},
		MinReplicas:     1,
		MaxReplicas:     1,
	}); err != nil {
		return fmt.Errorf("create database deployment %s: %w", n.Deployment, err)
	}
	made.deployment = true
	return waitHealthy(ctx, api, projectID, n.Deployment, opt)
}

func secretError(err error) error {
	return fmt.Errorf("store the database secret: %w (creating secrets needs a token with scope \"all\"; a deploy-scope token can only use databases that already exist)", err)
}

func waitHealthy(ctx context.Context, api API, projectID, name string, opt Options) error {
	deadline := time.Now().Add(opt.Timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opt.Poll):
		}
		st, err := api.GetDeploymentStatus(ctx, projectID, name)
		if err != nil {
			continue
		}
		if st.NomadStatus == "dead" {
			return fmt.Errorf("database %s failed to start; read its output with: heroctl logs %s", name, name)
		}
		if st.NomadStatus == "running" && st.Healthy {
			return nil
		}
	}
	return fmt.Errorf("database %s did not become healthy within %s; read its output with: heroctl logs %s", name, opt.Timeout, name)
}

const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GeneratePassword returns a random password that is safe inside a URL.
func GeneratePassword() (string, error) {
	b := make([]byte, 32)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("generate password: %w", err)
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b), nil
}
