package managed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

type fakeAPI struct {
	deployments []client.Deployment
	volumes     []client.Volume
	secrets     map[string]string
	created     []client.CreateDeploymentRequest
	createdVols []client.CreateVolumeRequest
	started     []string
	secretErr   error
	status      client.DeploymentStatus
	order       []string
	deleted     []string
}

func (f *fakeAPI) ListDeployments(context.Context, string) ([]client.Deployment, error) {
	return f.deployments, nil
}
func (f *fakeAPI) CreateDeployment(_ context.Context, _ string, r client.CreateDeploymentRequest) (*client.DeploymentCreated, error) {
	f.order = append(f.order, "deployment")
	f.created = append(f.created, r)
	return &client.DeploymentCreated{}, nil
}
func (f *fakeAPI) GetDeploymentStatus(context.Context, string, string) (*client.DeploymentStatus, error) {
	return &f.status, nil
}
func (f *fakeAPI) StartDeployment(_ context.Context, _, app string) (*client.DeploymentCreated, error) {
	f.started = append(f.started, app)
	return &client.DeploymentCreated{}, nil
}
func (f *fakeAPI) ListVolumes(context.Context, string) ([]client.Volume, error) {
	return f.volumes, nil
}
func (f *fakeAPI) CreateVolume(_ context.Context, _ string, r client.CreateVolumeRequest) (*client.Volume, error) {
	f.order = append(f.order, "volume")
	f.createdVols = append(f.createdVols, r)
	return &client.Volume{ID: "vol-1", Name: r.Name}, nil
}
func (f *fakeAPI) SetSecret(_ context.Context, _, key, value string) error {
	if f.secretErr != nil {
		return f.secretErr
	}
	f.order = append(f.order, "secret")
	if f.secrets == nil {
		f.secrets = map[string]string{}
	}
	f.secrets[key] = value
	return nil
}

func (f *fakeAPI) DeleteDeployment(_ context.Context, _, app string) error {
	f.deleted = append(f.deleted, "deployment:"+app)
	return nil
}
func (f *fakeAPI) DeleteVolume(_ context.Context, _, id string) error {
	f.deleted = append(f.deleted, "volume:"+id)
	return nil
}
func (f *fakeAPI) DeleteSecret(_ context.Context, _, key string) error {
	f.deleted = append(f.deleted, "secret:"+key)
	return nil
}

func fast() Options {
	return Options{Poll: time.Millisecond, Timeout: time.Second, Password: func() (string, error) { return "pw123", nil }}
}

func healthy() client.DeploymentStatus {
	return client.DeploymentStatus{NomadStatus: "running", Healthy: true}
}

func TestValidate_FillsDefaultsAndRejectsMistakes(t *testing.T) {
	got, err := Validate("shop", []Spec{{Name: "db", Engine: "postgres"}, {Name: "cache", Engine: "redis", SizeGB: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Version != "17" || got[0].SizeGB != 5 || got[0].Env != "DATABASE_URL" {
		t.Errorf("postgres defaults: %+v", got[0])
	}
	if got[1].Env != "REDIS_URL" || got[1].SizeGB != 2 {
		t.Errorf("redis: %+v", got[1])
	}
	for name, specs := range map[string][]Spec{
		"engine":     {{Name: "db", Engine: "oracle"}},
		"name":       {{Name: "DB_1", Engine: "postgres"}},
		"duplicate":  {{Name: "db", Engine: "postgres"}, {Name: "db", Engine: "mysql"}},
		"size":       {{Name: "db", Engine: "postgres", SizeGB: 5000}},
		"version":    {{Name: "db", Engine: "postgres", Version: "latest"}},
		"same var":   {{Name: "a", Engine: "postgres"}, {Name: "b", Engine: "mysql"}},
		"bad envvar": {{Name: "a", Engine: "postgres", Env: "1X"}},
		"too long":   {{Name: strings.Repeat("a", 60), Engine: "postgres"}},
	} {
		if _, err := Validate("shop", specs); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestProvision_CreatesSecretsVolumeAndAPrivateDeployment(t *testing.T) {
	api := &fakeAPI{status: healthy()}
	specs, _ := Validate("shop", []Spec{{Name: "db", Engine: "postgres", Version: "16", SizeGB: 7}})
	res, err := Provision(context.Background(), api, "p1", "shop", specs, fast())
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || !res[0].Created || res[0].EnvVar != "DATABASE_URL" || res[0].EnvValue != "secret:SHOP_DB_URL" {
		t.Fatalf("result: %+v", res)
	}
	if got := api.secrets["SHOP_DB_URL"]; got != "postgres://app:pw123@shop-db:5432/app?sslmode=disable" {
		t.Errorf("url secret = %q", got)
	}
	if api.secrets["SHOP_DB_PASSWORD"] != "pw123" {
		t.Errorf("password secret missing: %v", api.secrets)
	}
	if len(api.createdVols) != 1 || api.createdVols[0].Name != "shop-db-data" || api.createdVols[0].SizeGB != 7 {
		t.Errorf("volume: %+v", api.createdVols)
	}
	d := api.created[0]
	if d.AppName != "shop-db" || d.Image != "docker.io/library/postgres:16-alpine" || d.ServiceScope != "internal" ||
		d.HealthCheckType != "tcp" || d.Port != 5432 || d.MinReplicas != 1 || d.MaxReplicas != 1 {
		t.Errorf("deployment: %+v", d)
	}
	if d.Env["POSTGRES_PASSWORD"] != "secret:SHOP_DB_PASSWORD" {
		t.Errorf("the password must be a secret reference, got %q", d.Env["POSTGRES_PASSWORD"])
	}
	if len(d.Volumes) != 1 || d.Volumes[0].MountPath != "/var/lib/postgresql" || d.Volumes[0].VolumeID != "vol-1" {
		t.Errorf("volumes: %+v", d.Volumes)
	}
	if d.Labels[LabelEngine] != "postgres" || d.Labels[LabelFor] != "shop" {
		t.Errorf("labels: %v", d.Labels)
	}
	// Secrets exist before the deployment that references them.
	if strings.Join(api.order, ",") != "secret,secret,volume,deployment" {
		t.Errorf("order = %v", api.order)
	}
}

func TestProvision_ExistingDatabaseIsLeftAlone(t *testing.T) {
	api := &fakeAPI{status: healthy(), deployments: []client.Deployment{
		{AppName: "shop-db", Status: "running", Labels: map[string]string{LabelEngine: "postgres"}},
	}}
	specs, _ := Validate("shop", []Spec{{Name: "db", Engine: "postgres"}})
	res, err := Provision(context.Background(), api, "p1", "shop", specs, fast())
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Created || len(api.created) != 0 || len(api.secrets) != 0 || len(api.started) != 0 {
		t.Errorf("an existing database must not be touched: %+v %+v", res, api)
	}
	if res[0].EnvValue != "secret:SHOP_DB_URL" {
		t.Errorf("the app still gets the URL: %+v", res[0])
	}
}

func TestProvision_StoppedDatabaseIsStarted(t *testing.T) {
	api := &fakeAPI{status: healthy(), deployments: []client.Deployment{
		{AppName: "shop-db", Status: "stopped", Labels: map[string]string{LabelEngine: "postgres"}},
	}}
	specs, _ := Validate("shop", []Spec{{Name: "db", Engine: "postgres"}})
	if _, err := Provision(context.Background(), api, "p1", "shop", specs, fast()); err != nil {
		t.Fatal(err)
	}
	if len(api.started) != 1 || api.started[0] != "shop-db" {
		t.Errorf("started = %v", api.started)
	}
}

func TestProvision_RefusesToAdoptWhatItDidNotCreate(t *testing.T) {
	specs, _ := Validate("shop", []Spec{{Name: "db", Engine: "postgres"}})
	other := &fakeAPI{deployments: []client.Deployment{{AppName: "shop-db", Status: "running"}}}
	if _, err := Provision(context.Background(), other, "p1", "shop", specs, fast()); err == nil || !strings.Contains(err.Error(), "not a managed database") {
		t.Errorf("a foreign deployment of the same name: %v", err)
	}
	orphan := &fakeAPI{volumes: []client.Volume{{Name: "shop-db-data"}}}
	if _, err := Provision(context.Background(), orphan, "p1", "shop", specs, fast()); err == nil || !strings.Contains(err.Error(), "volumes destroy shop-db-data") {
		t.Errorf("an orphaned volume holds data under an unknown password: %v", err)
	}
	if len(orphan.secrets) != 0 || len(orphan.created) != 0 {
		t.Error("nothing may be created over an orphaned volume")
	}
}

func TestProvision_SecretScopeErrorSaysWhatToDo(t *testing.T) {
	api := &fakeAPI{secretErr: errors.New("status 403")}
	specs, _ := Validate("shop", []Spec{{Name: "db", Engine: "postgres"}})
	_, err := Provision(context.Background(), api, "p1", "shop", specs, fast())
	if err == nil || !strings.Contains(err.Error(), `scope "all"`) {
		t.Errorf("err = %v", err)
	}
	if len(api.createdVols) != 0 {
		t.Error("no volume may be created when the secrets cannot be stored")
	}
}

func TestProvision_DeadDatabaseFailsWithAPointerToTheLogs(t *testing.T) {
	api := &fakeAPI{status: client.DeploymentStatus{NomadStatus: "dead"}}
	specs, _ := Validate("shop", []Spec{{Name: "db", Engine: "mariadb"}})
	_, err := Provision(context.Background(), api, "p1", "shop", specs, fast())
	if err == nil || !strings.Contains(err.Error(), "heroctl logs shop-db") {
		t.Errorf("err = %v", err)
	}
}

func TestValidate_MySQLMeansMariaDB(t *testing.T) {
	got, err := Validate("shop", []Spec{{Name: "db", Engine: "mysql"}})
	if err != nil || got[0].Engine != "mariadb" || got[0].Version != "11.4" {
		t.Errorf("got %+v err %v", got, err)
	}
}

func TestEngines_RedisHasNoPasswordAndMariaDBNoRoot(t *testing.T) {
	if got := Engines["redis"].URL("shop-cache", "x"); got != "redis://shop-cache:6379" {
		t.Errorf("redis url = %q", got)
	}
	env := Engines["mariadb"].Env("secret:K")
	if env["MARIADB_PASSWORD"] != "secret:K" || env["MARIADB_RANDOM_ROOT_PASSWORD"] != "1" {
		t.Errorf("mysql env = %v", env)
	}
}

func TestGeneratePassword_IsLongRandomAndURLSafe(t *testing.T) {
	a, _ := GeneratePassword()
	b, _ := GeneratePassword()
	if len(a) != 32 || a == b || strings.ContainsAny(a, "@:/?#%&= ") {
		t.Errorf("a=%q b=%q", a, b)
	}
}

func TestProvision_AFailedDatabaseIsTakenBackSoTheNextDeployCanRetry(t *testing.T) {
	api := &fakeAPI{status: client.DeploymentStatus{NomadStatus: "dead"}}
	specs, _ := Validate("shop", []Spec{{Name: "db", Engine: "postgres"}})
	if _, err := Provision(context.Background(), api, "p1", "shop", specs, fast()); err == nil {
		t.Fatal("expected the dead database to fail the deploy")
	}
	got := strings.Join(api.deleted, ",")
	for _, want := range []string{"deployment:shop-db", "volume:vol-1", "secret:SHOP_DB_PASSWORD", "secret:SHOP_DB_URL"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s was left behind; deleted = %s", want, got)
		}
	}
}

func TestProvision_NothingIsDeletedWhenTheDatabaseWorked(t *testing.T) {
	api := &fakeAPI{status: healthy()}
	specs, _ := Validate("shop", []Spec{{Name: "db", Engine: "postgres"}})
	if _, err := Provision(context.Background(), api, "p1", "shop", specs, fast()); err != nil {
		t.Fatal(err)
	}
	if len(api.deleted) != 0 {
		t.Errorf("deleted = %v", api.deleted)
	}
}
