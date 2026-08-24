package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func TestFormatCredits(t *testing.T) {
	tests := []struct {
		name string
		in   client.Credits
		want string
	}{
		{"whole number", client.Credits{MilliCredits: 12000}, "12.000"},
		{"fractional", client.Credits{MilliCredits: 12500}, "12.500"},
		{"sub-credit", client.Credits{MilliCredits: 7}, "0.007"},
		{"negative balance", client.Credits{MilliCredits: -1500}, "-1.500"},
		{"large", client.Credits{MilliCredits: 1234567}, "1234.567"},
		// Falls back to the float only when the integer field is absent.
		{"fallback to float", client.Credits{Credits: 3.5}, "3.500"},
		{"zero", client.Credits{}, "0.000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatCredits(&tc.in); got != tc.want {
				t.Errorf("formatCredits(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// MilliCredits is the integer the API bills in; formatting from it must not
// inherit float64 rounding error. 0.1+0.2 style drift is the failure mode.
func TestFormatCreditsPrefersIntegerOverFloat(t *testing.T) {
	// A float64 that does not round-trip cleanly at 3 decimals.
	c := client.Credits{MilliCredits: 1003, Credits: 1.0029999999999999}
	if got, want := formatCredits(&c), "1.003"; got != want {
		t.Errorf("formatCredits = %q, want %q (must use MilliCredits)", got, want)
	}
}

func TestCreditsCmd(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/orgs/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"ID": "org-1", "Name": "demo"})
		case "/api/v1/orgs/org-1/credits":
			_ = json.NewEncoder(w).Encode(map[string]any{"credits": 12.5, "milli_credits": 12500})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	cmd := creditsCmd(newTestDeps(srv))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := out.String()
	for _, want := range []string{"demo", "org-1", "12.500"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q missing %q", got, want)
		}
	}
	if len(gotPaths) != 2 {
		t.Errorf("requested %v, want the org then its credits", gotPaths)
	}
}

func TestDeploymentError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantHint string
	}{
		{
			name:     "vm cap points at the deployment list",
			err:      errors.New("org vm cap reached"),
			wantHint: "heroctl deployments list --project demo",
		},
		{
			name:     "insufficient credits points at the balance",
			err:      errors.New("insufficient credits"),
			wantHint: "heroctl credits",
		},
		{
			name:     "credit wording varies and is matched case-insensitively",
			err:      errors.New("Not enough Credit balance for this deployment"),
			wantHint: "heroctl credits",
		},
		{
			name:     "anything else keeps the generic prefix",
			err:      errors.New("boom"),
			wantHint: "create deployment: boom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deploymentError(tc.err, "demo").Error()
			if !strings.Contains(got, tc.wantHint) {
				t.Errorf("deploymentError = %q, want it to contain %q", got, tc.wantHint)
			}
			// The server's own message must survive in every branch.
			if !strings.Contains(strings.ToLower(got), strings.ToLower(tc.err.Error())) {
				t.Errorf("deploymentError = %q dropped the original message %q", got, tc.err)
			}
		})
	}
}

// The generic branch must keep wrapping so errors.Is/As still work upstream.
func TestDeploymentErrorWrapsUnknownErrors(t *testing.T) {
	sentinel := errors.New("sentinel")
	if !errors.Is(deploymentError(sentinel, "demo"), sentinel) {
		t.Error("generic branch must wrap the original error")
	}
}
