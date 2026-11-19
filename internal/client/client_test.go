package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/auth"
)

type seen struct {
	method, path, auth, body string
}

// serve answers every request with status and body and records what came in.
func serve(t *testing.T, status int, body string) (*Client, *[]seen) {
	t.Helper()
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), string(b)})
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "auth.invalid", "cid", &auth.Token{AccessToken: "tok", Expiry: time.Now().Add(time.Hour)}), &got
}

func TestEndpoints_UseTheRightMethodPathAndBody(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name         string
		call         func(c *Client) error
		method, path string
		bodyHas      string
	}{
		{"projects list", func(c *Client) error { _, err := c.ListProjects(ctx); return err }, "GET", "/api/v1/projects", ""},
		{"project create", func(c *Client) error { _, err := c.CreateProject(ctx, "p"); return err }, "POST", "/api/v1/projects", `"name":"p"`},
		{"project delete", func(c *Client) error { return c.DeleteProject(ctx, "p1") }, "DELETE", "/api/v1/projects/p1", ""},
		{"deployments list", func(c *Client) error { _, err := c.ListDeployments(ctx, "p1"); return err }, "GET", "/api/v1/projects/p1/deployments", ""},
		{"deployment status", func(c *Client) error { _, err := c.GetDeploymentStatus(ctx, "p1", "web"); return err }, "GET", "/api/v1/projects/p1/deployments/web/status", ""},
		{"deployment stop", func(c *Client) error { return c.StopDeployment(ctx, "p1", "web") }, "POST", "/api/v1/projects/p1/deployments/web/stop", ""},
		{"deployment delete", func(c *Client) error { return c.DeleteDeployment(ctx, "p1", "web") }, "DELETE", "/api/v1/projects/p1/deployments/web", ""},
		{"deployment start", func(c *Client) error { _, err := c.StartDeployment(ctx, "p1", "web"); return err }, "POST", "/api/v1/projects/p1/deployments/web/start", ""},
		{"deployment restart", func(c *Client) error { _, err := c.RestartDeployment(ctx, "p1", "web"); return err }, "POST", "/api/v1/projects/p1/deployments/web/restart", ""},
		{"deployment create", func(c *Client) error {
			_, err := c.CreateDeployment(ctx, "p1", CreateDeploymentRequest{AppName: "web", Image: "i", CPU: 1})
			return err
		}, "POST", "/api/v1/projects/p1/deployments", `"app_name":"web"`},
		{"secret set", func(c *Client) error { return c.SetSecret(ctx, "p1", "K", "v") }, "POST", "/api/v1/projects/p1/secrets", `"key":"K"`},
		{"secret list", func(c *Client) error { _, err := c.ListSecrets(ctx, "p1"); return err }, "GET", "/api/v1/projects/p1/secrets", ""},
		{"secret delete", func(c *Client) error { return c.DeleteSecret(ctx, "p1", "K") }, "DELETE", "/api/v1/projects/p1/secrets/K", ""},
		{"volume create", func(c *Client) error {
			_, err := c.CreateVolume(ctx, "p1", CreateVolumeRequest{Name: "v", SizeGB: 2})
			return err
		}, "POST", "/api/v1/projects/p1/volumes", `"size_gb":2`},
		{"volume list", func(c *Client) error { _, err := c.ListVolumes(ctx, "p1"); return err }, "GET", "/api/v1/projects/p1/volumes", ""},
		{"volume delete", func(c *Client) error { return c.DeleteVolume(ctx, "p1", "v9") }, "DELETE", "/api/v1/projects/p1/volumes/v9", ""},
		{"volume resize", func(c *Client) error { return c.ResizeVolume(ctx, "p1", "v9", 20) }, "POST", "/api/v1/projects/p1/volumes/v9/resize", `"size_gb":20`},
		{"token create", func(c *Client) error { _, err := c.CreatePAT(ctx, "ci", "deploy"); return err }, "POST", "/api/v1/tokens", `"scope":"deploy"`},
		{"token list", func(c *Client) error { _, err := c.ListPATs(ctx); return err }, "GET", "/api/v1/tokens", ""},
		{"token delete", func(c *Client) error { return c.DeletePAT(ctx, "t1") }, "DELETE", "/api/v1/tokens/t1", ""},
		{"token create held to a project", func(c *Client) error {
			_, err := c.CreatePAT(ctx, "ci", "read", PATOptions{ExpiresInDays: 30, ProjectID: "p1"})
			return err
		}, "POST", "/api/v1/tokens", `"expires_in_days":30,"name":"ci","project_id":"p1","scope":"read"`},
		{"owner transfer", func(c *Client) error { return c.TransferOwnership(ctx, "u2") }, "POST", "/api/v1/orgs/me/owner", `"principal_id":"u2"`},
		{"team create", func(c *Client) error { _, err := c.CreateTeam(ctx, "Backend"); return err }, "POST", "/api/v1/teams", `"name":"Backend"`},
		{"team delete", func(c *Client) error { return c.DeleteTeam(ctx, "t1") }, "DELETE", "/api/v1/teams/t1", ""},
		{"team member add", func(c *Client) error { return c.SetTeamMember(ctx, "t1", "u1", true) }, "PUT", "/api/v1/teams/t1/members/u1", ""},
		{"team member remove", func(c *Client) error { return c.SetTeamMember(ctx, "t1", "u1", false) }, "DELETE", "/api/v1/teams/t1/members/u1", ""},
		{"team project grant", func(c *Client) error { return c.SetTeamProject(ctx, "t1", "p1", "editor") }, "PUT", "/api/v1/projects/p1/teams/t1", `"role":"editor"`},
		{"team project revoke", func(c *Client) error { return c.SetTeamProject(ctx, "t1", "p1", "") }, "DELETE", "/api/v1/projects/p1/teams/t1", ""},
		{"env set", func(c *Client) error { return c.SetDeploymentEnv(ctx, "p1", "web", map[string]string{"A": "b"}) }, "POST", "/api/v1/projects/p1/deployments/web/env", `"A":"b"`},
		{"events", func(c *Client) error { _, err := c.ListDeploymentEvents(ctx, "p1", "web", 5); return err }, "GET", "/api/v1/projects/p1/deployments/web/events?limit=5", ""},
		{"metrics", func(c *Client) error { _, err := c.GetDeploymentMetrics(ctx, "p1", "web", "6h"); return err }, "GET", "/api/v1/projects/p1/deployments/web/metrics?range=6h", ""},
		{"alerts list", func(c *Client) error { _, err := c.ListDeploymentAlerts(ctx, "p1", "web"); return err }, "GET", "/api/v1/projects/p1/deployments/web/alerts", ""},
		{"alert delete", func(c *Client) error { return c.DeleteDeploymentAlert(ctx, "p1", "web", "a1") }, "DELETE", "/api/v1/projects/p1/deployments/web/alerts/a1", ""},
		{"audit", func(c *Client) error { _, err := c.ListAudit(ctx, 10, 20); return err }, "GET", "/api/v1/audit?limit=10&offset=20", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, got := serve(t, 200, `{"alerts":[],"events":[],"entries":[],"series":[],"revisions":[]}`)
			// List endpoints that return a bare array accept an empty one.
			if strings.Contains(tc.name, "list") && !strings.Contains(tc.name, "alerts") {
				c, got = serve(t, 200, `[]`)
			}
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if len(*got) != 1 {
				t.Fatalf("%d requests", len(*got))
			}
			r := (*got)[0]
			if r.method != tc.method || r.path != tc.path {
				t.Errorf("%s %s, want %s %s", r.method, r.path, tc.method, tc.path)
			}
			if r.auth != "Bearer tok" {
				t.Errorf("Authorization = %q", r.auth)
			}
			if tc.bodyHas != "" && !strings.Contains(r.body, tc.bodyHas) {
				t.Errorf("body %q lacks %s", r.body, tc.bodyHas)
			}
		})
	}
}

func TestErrors_TheServersMessageSurvives(t *testing.T) {
	c, _ := serve(t, 402, `{"error":"insufficient balance"}`)
	if _, err := c.ListProjects(context.Background()); err == nil || err.Error() != "insufficient balance" {
		t.Errorf("err = %v", err)
	}
	c, _ = serve(t, 500, `<html>`)
	if _, err := c.ListProjects(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("a body that is not JSON: %v", err)
	}
}

func TestErrors_UnverifiedDomainsComeBackWithTheirRecords(t *testing.T) {
	c, _ := serve(t, 422, `{"error":"domains not verified","domain_verification":[{"domain":"a.example","record_name":"_hero-challenge.a.example","record_value":"hero-verification=x"}]}`)
	_, err := c.CreateDeployment(context.Background(), "p1", CreateDeploymentRequest{AppName: "web"})
	var dv *DomainVerificationError
	if !errors.As(err, &dv) || len(dv.Pending) != 1 || dv.Pending[0].Domain != "a.example" {
		t.Fatalf("err = %#v", err)
	}
}

func TestListings_NeverReturnNil(t *testing.T) {
	c, _ := serve(t, 200, `null`)
	if d, err := c.ListDeployments(context.Background(), "p"); err != nil || d == nil {
		t.Errorf("deployments %v %v", d, err)
	}
	if s, err := c.ListSecrets(context.Background(), "p"); err != nil || s == nil {
		t.Errorf("secrets %v %v", s, err)
	}
}

func TestBadResponse_IsAnErrorNotAZeroValue(t *testing.T) {
	c, _ := serve(t, 200, `not json`)
	if _, err := c.GetOrg(context.Background()); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v", err)
	}
}

func TestExpiredTokenIsRefreshedBeforeTheCall(t *testing.T) {
	// The identity provider is https; the refresh itself is covered in package auth.
	// Here an expired token with an unreachable provider must stop the call with
	// the instruction to log in again, and must not send the request.
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	c := New(srv.URL, "127.0.0.1:1", "cid", &auth.Token{AccessToken: "old", RefreshToken: "r", Expiry: time.Now().Add(-time.Hour)})
	_, err := c.ListProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "heroctl login") {
		t.Fatalf("err = %v", err)
	}
	if hits != 0 {
		t.Errorf("the API was called %d times with an expired token", hits)
	}
}

func TestJSONRoundTrip_CreateDeploymentRequestOmitsWhatIsEmpty(t *testing.T) {
	b, _ := json.Marshal(CreateDeploymentRequest{AppName: "a"})
	for _, absent := range []string{"labels", "volumes", "verify_domains", "service_scope"} {
		if strings.Contains(string(b), absent) {
			t.Errorf("%s must be omitted when empty: %s", absent, b)
		}
	}
}

func TestListTeams_ReadsTheWrappedList(t *testing.T) {
	c, got := serve(t, 200, `{"teams":[{"id":"t1","name":"Backend","members":[{"principal_id":"u1","email":"a@example.test"}],"projects":[{"project_id":"p1","project_name":"shop","role":"viewer"}]}]}`)
	teams, err := c.ListTeams(context.Background())
	if err != nil || len(teams) != 1 || teams[0].Name != "Backend" || teams[0].Projects[0].Role != "viewer" {
		t.Fatalf("teams = %+v, %v", teams, err)
	}
	if r := (*got)[0]; r.method != "GET" || r.path != "/api/v1/teams" {
		t.Errorf("%s %s", r.method, r.path)
	}
}
