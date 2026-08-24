package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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

// runCmd executes a command against a stub server and returns what it printed.
func runCmd(t *testing.T, srv *httptest.Server, build func(*Deps) *cobra.Command, args ...string) string {
	t.Helper()
	cmd := build(newTestDeps(srv))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return out.String()
}

func TestCreditsLedger_ShowsSignAndReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/credits/ledger" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{"id": "1", "delta_milli_credits": 11000000, "delta_credits": 11000.0,
					"reason": "mollie payment tr_1", "created_at": "2026-08-24T10:00:00Z"},
				{"id": "2", "delta_milli_credits": -84, "delta_credits": -0.084,
					"reason":     "usage: 1 vCPU, 1024 MB RAM, 0 GB storage for 5m0s",
					"created_at": "2026-08-24T10:05:00Z"},
			},
			"total": 2, "limit": 20, "offset": 0,
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, creditsLedgerCmd)

	// The sign is what distinguishes a top-up from a usage charge at a glance.
	if !strings.Contains(out, "+11000.000") {
		t.Errorf("a top-up must show a leading +, got:\n%s", out)
	}
	if !strings.Contains(out, "-0.084") {
		t.Errorf("a usage charge must show as negative, got:\n%s", out)
	}
	if !strings.Contains(out, "mollie payment tr_1") {
		t.Errorf("the reason must be shown, got:\n%s", out)
	}
}

func TestCreditsLedger_PassesPagination(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{}, "total": 0})
	}))
	defer srv.Close()

	runCmd(t, srv, creditsLedgerCmd, "--limit", "5", "--offset", "10")

	if !strings.Contains(gotQuery, "limit=5") || !strings.Contains(gotQuery, "offset=10") {
		t.Errorf("pagination not forwarded, query was %q", gotQuery)
	}
}

func TestCreditsLedger_EmptyIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{}, "total": 0})
	}))
	defer srv.Close()

	out := runCmd(t, srv, creditsLedgerCmd)

	if !strings.Contains(out, "No ledger entries") {
		t.Errorf("an empty ledger should say so, got:\n%s", out)
	}
}

func TestCreditsLedger_HintsAtTheNextPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{{"id": "1", "delta_credits": 1.0, "reason": "x",
				"created_at": "2026-08-24T10:00:00Z"}},
			"total": 50, "limit": 1, "offset": 0,
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, creditsLedgerCmd)

	// Without this a user sees 1 of 50 rows and no way to know more exist.
	if !strings.Contains(out, "--offset 1") {
		t.Errorf("a truncated ledger must point at the next page, got:\n%s", out)
	}
}

func TestCreditsPackages_ShowsPriceAndBonus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/payments/packages" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"packages": []map[string]any{
				{"id": "starter", "label": "Starter", "amount_eur": "5.00",
					"milli_credits": 5000000, "credits": 5000, "bonus_percent": 0},
				{"id": "pro", "label": "Pro", "amount_eur": "25.00",
					"milli_credits": 30000000, "credits": 30000, "bonus_percent": 20},
			},
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, creditsPackagesCmd)

	if !strings.Contains(out, "5.00 EUR") || !strings.Contains(out, "+20%") {
		t.Errorf("price and bonus must be shown, got:\n%s", out)
	}
	// A zero bonus must not read as "+0%", which looks like a broken deal.
	if strings.Contains(out, "+0%") {
		t.Errorf("a package without a bonus should show a dash, got:\n%s", out)
	}
}

func TestCreditsTopup_PrintsTheCheckoutURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["package"] != "pro" {
			t.Errorf("package not forwarded, got %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"payment_id": "tr_1", "checkout_url": "https://mollie.test/tr_1",
			"package": "pro", "amount_eur": "25.00",
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, creditsTopupCmd, "pro")

	if !strings.Contains(out, "https://mollie.test/tr_1") {
		t.Errorf("the checkout URL is the whole point of the command, got:\n%s", out)
	}
}

func TestCreditsTopup_MissingBillingProfileIsActionable(t *testing.T) {
	// hero-api answers 428 when no billing profile exists. "checkout failed"
	// would leave the user guessing at something one command fixes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionRequired)
		_, _ = w.Write([]byte(`{"error":"a billing profile is required before purchasing credits"}`))
	}))
	defer srv.Close()

	cmd := creditsTopupCmd(newTestDeps(srv))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"pro"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for a missing billing profile")
	}
	if !strings.Contains(err.Error(), "heroctl billing set") {
		t.Errorf("the error must name the command that fixes it, got: %v", err)
	}
}

func TestCreditsPayments_ListsHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"payments": []map[string]any{
				{"id": "u1", "mollie_id": "tr_1", "milli_credits": 30000000,
					"status": "paid", "created_at": "2026-08-24T10:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, creditsPaymentsCmd)

	if !strings.Contains(out, "tr_1") || !strings.Contains(out, "paid") {
		t.Errorf("payment history incomplete, got:\n%s", out)
	}
	if !strings.Contains(out, "30000") {
		t.Errorf("credits should be shown in whole credits, not milli, got:\n%s", out)
	}
}
