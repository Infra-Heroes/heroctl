package client

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestMoreEndpoints_UseTheRightMethodPathAndBody(t *testing.T) {
	ctx := context.Background()
	i32 := func(v int32) *int32 { return &v }
	for _, tc := range []struct {
		name         string
		call         func(c *Client) error
		method, path string
		bodyHas      string
		reply        string
	}{
		{"signup", func(c *Client) error { _, err := c.Signup(ctx, SignupRequest{}); return err }, "POST", "/api/v1/signup", "", `{}`},
		{"org me", func(c *Client) error { _, err := c.GetOrg(ctx); return err }, "GET", "/api/v1/orgs/me", "", `{}`},
		{"org create", func(c *Client) error { _, err := c.CreateOrg(ctx, "acme"); return err }, "POST", "/api/v1/orgs", `"name":"acme"`, `{}`},
		{"balance", func(c *Client) error { _, err := c.GetBalance(ctx, "o1"); return err }, "GET", "/api/v1/orgs/o1/balance", "", `{}`},
		{"ledger", func(c *Client) error { _, err := c.GetLedger(ctx, 5, 10); return err }, "GET", "/api/v1/balance/ledger?limit=5&offset=10", "", `{}`},
		{"pricing", func(c *Client) error { _, err := c.GetPricing(ctx); return err }, "GET", "/api/v1/pricing", "", `{}`},
		{"payments", func(c *Client) error { _, err := c.ListPayments(ctx); return err }, "GET", "/api/v1/payments", "", `{"payments":[]}`},
		{"checkout", func(c *Client) error { _, err := c.CreateCheckout(ctx, "10.00"); return err }, "POST", "/api/v1/payments/checkout", `10.00`, `{}`},
		{"billing profile get", func(c *Client) error { _, err := c.GetBillingProfile(ctx); return err }, "GET", "/api/v1/billing/profile", "", `{}`},
		{"billing profile put", func(c *Client) error { _, err := c.PutBillingProfile(ctx, BillingProfile{Name: "A"}); return err }, "PUT", "/api/v1/billing/profile", `"name":"A"`, `{}`},
		{"limits", func(c *Client) error { _, err := c.UpdateOrgLimits(ctx, "o1", i32(3), nil, nil, nil); return err }, "PATCH", "/api/v1/orgs/o1/limits", "", `{}`},
		{"deployment get", func(c *Client) error { _, err := c.GetDeployment(ctx, "p1", "web"); return err }, "GET", "/api/v1/projects/p1/deployments/web", "", `{}`},
		{"resources", func(c *Client) error { _, err := c.UpdateDeploymentResources(ctx, "p1", "web", 2, 1024); return err }, "POST", "/api/v1/projects/p1/deployments/web/resources", `"memory_mb":1024`, `{}`},
		{"registry credentials", func(c *Client) error { _, err := c.RegistryCredentials(ctx); return err }, "POST", "/api/v1/registry/credentials", "", `{}`},
		{"snapshots list", func(c *Client) error { _, err := c.ListSnapshots(ctx, "p1", "v1"); return err }, "GET", "/api/v1/projects/p1/volumes/v1/snapshots", "", `{"snapshots":[]}`},
		{"snapshot create", func(c *Client) error { _, err := c.CreateSnapshot(ctx, "p1", "v1", "s1"); return err }, "POST", "/api/v1/projects/p1/volumes/v1/snapshots", `"name":"s1"`, `{"name":"s1"}`},
		{"snapshot delete", func(c *Client) error { return c.DeleteSnapshot(ctx, "p1", "v1", "s1") }, "DELETE", "/api/v1/projects/p1/volumes/v1/snapshots/s1", "", ``},
		{"snapshot restore", func(c *Client) error { return c.RestoreSnapshot(ctx, "p1", "v1", "s1") }, "POST", "/api/v1/projects/p1/volumes/v1/snapshots/s1/restore", "", ``},
		{"snapshot policy get", func(c *Client) error { _, err := c.GetSnapshotPolicy(ctx, "p1", "v1"); return err }, "GET", "/api/v1/projects/p1/volumes/v1/snapshot-policy", "", `{}`},
		{"snapshot policy put", func(c *Client) error {
			_, err := c.PutSnapshotPolicy(ctx, "p1", "v1", SnapshotPolicyRequest{})
			return err
		}, "PUT", "/api/v1/projects/p1/volumes/v1/snapshot-policy", "", `{}`},
		{"org members", func(c *Client) error { _, err := c.ListOrgMembers(ctx); return err }, "GET", "/api/v1/members", "", `[]`},
		{"member role", func(c *Client) error { return c.UpdateMemberRole(ctx, "u1", "admin") }, "POST", "/api/v1/members/u1/role", `"admin"`, ``},
		{"member remove", func(c *Client) error { return c.RemoveOrgMember(ctx, "u1") }, "DELETE", "/api/v1/members/u1", "", ``},
		{"project members", func(c *Client) error { _, err := c.ListProjectMembers(ctx, "p1"); return err }, "GET", "/api/v1/projects/p1/members", "", `[]`},
		{"project member upsert", func(c *Client) error { return c.UpsertProjectMember(ctx, "p1", "u1", "viewer") }, "PUT", "/api/v1/projects/p1/members/u1", `"viewer"`, ``},
		{"project member remove", func(c *Client) error { return c.RemoveProjectMember(ctx, "p1", "u1") }, "DELETE", "/api/v1/projects/p1/members/u1", "", ``},
		{"invite create", func(c *Client) error {
			_, err := c.CreateInvitation(ctx, CreateInvitationRequest{Email: "a@example.com", OrgRole: "member"})
			return err
		}, "POST", "/api/v1/invitations", `a@example.com`, `{}`},
		{"invitations list", func(c *Client) error { _, err := c.ListInvitations(ctx); return err }, "GET", "/api/v1/invitations", "", `[]`},
		{"invite revoke", func(c *Client) error { return c.RevokeInvitation(ctx, "i1") }, "DELETE", "/api/v1/invitations/i1", "", ``},
		{"invite accept", func(c *Client) error { return c.AcceptInvitation(ctx, "tok9") }, "POST", "/api/v1/invitations/tok9/accept", "", ``},
		{"domain challenge", func(c *Client) error { _, err := c.DomainChallenge(ctx, "p1", "a.example"); return err }, "GET", "/api/v1/projects/p1/domains/challenge?domain=a.example", "", `{}`},
		{"domain verify", func(c *Client) error { _, err := c.VerifyDomain(ctx, "p1", "a.example"); return err }, "POST", "/api/v1/projects/p1/domains/verify", `a.example`, `{}`},
		{"schedule get", func(c *Client) error { _, err := c.GetDeploymentSchedule(ctx, "p1", "web"); return err }, "GET", "/api/v1/projects/p1/deployments/web/schedule", "", `{}`},
		{"schedule delete", func(c *Client) error { return c.DeleteDeploymentSchedule(ctx, "p1", "web") }, "DELETE", "/api/v1/projects/p1/deployments/web/schedule", "", ``},
		{"revisions", func(c *Client) error { _, err := c.ListDeploymentRevisions(ctx, "p1", "web", 7); return err }, "GET", "/api/v1/projects/p1/deployments/web/revisions?limit=7", "", `{"revisions":[]}`},
		{"rollback", func(c *Client) error { _, err := c.RollbackDeployment(ctx, "p1", "web", "r1"); return err }, "POST", "/api/v1/projects/p1/deployments/web/revisions/r1/rollback", "", `{}`},
		{"alert put", func(c *Client) error {
			_, err := c.PutDeploymentAlert(ctx, "p1", "web", DeploymentAlertRequest{Metric: "cpu", Threshold: 90})
			return err
		}, "PUT", "/api/v1/projects/p1/deployments/web/alerts", `"metric":"cpu"`, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, got := serve(t, 200, tc.reply)
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
			if tc.bodyHas != "" && !strings.Contains(r.body, tc.bodyHas) {
				t.Errorf("body %q lacks %s", r.body, tc.bodyHas)
			}
		})
	}
}

func TestSignupNeedsNoToken(t *testing.T) {
	c, got := serve(t, 200, `{}`)
	if _, err := c.Signup(context.Background(), SignupRequest{}); err != nil {
		t.Fatal(err)
	}
	if (*got)[0].auth != "" {
		t.Errorf("signup sent an Authorization header: %q", (*got)[0].auth)
	}
}

func TestStreamLogs_ReturnsTheBodyAndTheFollowFlag(t *testing.T) {
	c, got := serve(t, 200, "line one\nline two\n")
	rc, err := c.StreamLogs(context.Background(), "p1", "web", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)
	if string(b) != "line one\nline two\n" {
		t.Errorf("body = %q", b)
	}
	if p := (*got)[0].path; !strings.Contains(p, "/deployments/web/logs") || !strings.Contains(p, "follow=true") {
		t.Errorf("path = %s", p)
	}
}

func TestStreamLogs_ServerErrorIsReported(t *testing.T) {
	c, _ := serve(t, 500, `{"error":"no running allocation"}`)
	if _, err := c.StreamLogs(context.Background(), "p1", "web", false); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v", err)
	}
}
