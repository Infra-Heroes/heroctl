package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func TestDomainVerificationErrorNamesTheRecords(t *testing.T) {
	err := &client.DomainVerificationError{
		Message: "1 custom domain(s) are not verified yet",
		Pending: []client.DomainChallenge{{Domain: "shop.example.com", RecordType: "TXT",
			RecordName: "_hero-challenge.shop.example.com", RecordValue: "hero-verification=abc"}},
	}
	text := err.Error()
	for _, want := range []string{"_hero-challenge.shop.example.com", "hero-verification=abc", "heroctl domains verify"} {
		if !strings.Contains(text, want) {
			t.Errorf("error text lacks %q:\n%s", want, text)
		}
	}
}

// A refused deploy must reach the user with its records intact; wrapping the
// error in "create deployment: ..." is fine, losing the records is not.
func TestDeploymentErrorKeepsDomainRecords(t *testing.T) {
	original := &client.DomainVerificationError{Message: "x", Pending: []client.DomainChallenge{{RecordName: "n", RecordValue: "v"}}}
	got := deploymentError(original, "app")
	var domainErr *client.DomainVerificationError
	if !errors.As(got, &domainErr) || len(domainErr.Pending) != 1 {
		t.Fatalf("the records were lost: %v", got)
	}
}

func TestClientSurfacesDomainRecordsOnA422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "1 custom domain(s) are not verified yet",
			"domain_verification": []map[string]any{{"domain": "a.example.com", "record_type": "TXT",
				"record_name": "_hero-challenge.a.example.com", "record_value": "hero-verification=tok", "verified": false}},
		})
	}))
	defer srv.Close()

	_, err := newTestDeps(srv).Client.CreateDeployment(context.Background(), "p", client.CreateDeploymentRequest{AppName: "a", VerifyDomains: true})

	var domainErr *client.DomainVerificationError
	if !errors.As(err, &domainErr) {
		t.Fatalf("err = %v, want a DomainVerificationError", err)
	}
	if domainErr.Pending[0].RecordValue != "hero-verification=tok" {
		t.Errorf("pending = %+v", domainErr.Pending)
	}
}

func TestVerifyDomainFlagIsSentOnlyWhenSet(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newTestDeps(srv).Client

	_, _ = c.CreateDeployment(context.Background(), "p", client.CreateDeploymentRequest{AppName: "a"})
	if _, has := body["verify_domains"]; has {
		t.Error("verify_domains must be absent unless asked for: older servers and unchanged pipelines see the request they always saw")
	}
	_, _ = c.CreateDeployment(context.Background(), "p", client.CreateDeploymentRequest{AppName: "a", VerifyDomains: true})
	if body["verify_domains"] != true {
		t.Errorf("verify_domains = %v, want true", body["verify_domains"])
	}
}

func TestDomainsCommands(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.URL.Path == "/api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"domain": "shop.example.com", "record_type": "TXT",
				"record_name": "_hero-challenge.shop.example.com", "record_value": "hero-verification=t", "verified": false})
		}
	}))
	defer srv.Close()

	for _, args := range [][]string{
		{"challenge", "shop.example.com", "--project", "app"},
		{"verify", "shop.example.com", "--project", "app"},
	} {
		cmd := domainsCmd(newTestDeps(srv))
		cmd.SetArgs(args)
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	want := []string{
		"GET /api/v1/projects/proj-1/domains/challenge?domain=shop.example.com",
		"POST /api/v1/projects/proj-1/domains/verify",
	}
	got := strings.Join(paths, "|")
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing request %q in %s", w, got)
		}
	}
}
