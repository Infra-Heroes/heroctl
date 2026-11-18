package cmd

import (
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func TestNotifications(t *testing.T) {
	s := &opsServer{}
	srv := newOpsServer(t, s)
	deps := newTestDeps(srv)
	for _, args := range [][]string{
		{"list"},
		{"add", "new", "--kind", "webhook", "--url", "https://example.com/h"},
		{"test", "ops"},
		{"delete", "ops"},
	} {
		if err := execCmd(t, notificationsCmd(deps), args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if b := s.bodies["POST /api/v1/orgs/me/channels"]; b["kind"] != "webhook" || b["url"] != "https://example.com/h" {
		t.Errorf("body = %v", b)
	}
	for _, want := range []string{"POST /api/v1/orgs/me/channels/ch-1/test", "DELETE /api/v1/orgs/me/channels/ch-1"} {
		if !called(s.calls, want) {
			t.Errorf("missing %s in %v", want, s.calls)
		}
	}
}

func TestNotificationsRefusals(t *testing.T) {
	s := &opsServer{}
	deps := newTestDeps(newOpsServer(t, s))
	for _, args := range [][]string{
		{"add", "x", "--kind", "sms", "--url", "https://e.com"},
		{"add", "x", "--kind", "slack", "--url", "http://e.com"},
		{"test", "missing"},
		{"test", "hook"},
	} {
		if err := execCmd(t, notificationsCmd(deps), args...); err == nil {
			t.Errorf("%v should fail", args)
		}
	}
}

func TestLastDelivery(t *testing.T) {
	if lastDelivery(client.NotificationChannel{}) != "-" || lastDelivery(client.NotificationChannel{LastError: "x"}) != "failed: x" {
		t.Error("lastDelivery")
	}
}

func TestAlertsSet_Channels(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want []any
	}{{"ops,hook", []any{"ch-1", "ch-2"}}, {"none", []any{}}} {
		s := &opsServer{}
		srv := newOpsServer(t, s)
		if err := execCmd(t, deploymentsAlertsCmd(newTestDeps(srv)), "set", "web", "--project", "app", "--metric", "unavailable", "--channel", tc.arg); err != nil {
			t.Fatal(err)
		}
		got, _ := s.bodies["PUT /api/v1/projects/proj-1/deployments/web/alerts"]["channels"].([]any)
		if len(got) != len(tc.want) || (len(got) > 0 && got[0] != tc.want[0]) {
			t.Errorf("%s: channels = %v", tc.arg, got)
		}
	}
	s := &opsServer{}
	if err := execCmd(t, deploymentsAlertsCmd(newTestDeps(newOpsServer(t, s))), "set", "web", "--project", "app", "--metric", "unavailable", "--channel", "nope"); err == nil {
		t.Error("unknown channel must fail")
	}
}
