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
)

func TestFormatEuro(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want string
	}{
		{"whole euros", 12_000_000, "12.00"},
		{"cents", 12_500_000, "12.50"},
		{"rounds half away from zero", 12_505_000, "12.51"},
		{"sub-cent rounds down", 12_504_999, "12.50"},
		{"negative", -1_500_000, "-1.50"},
		{"zero", 0, "0.00"},
		// A balance of a few micro-euro rounds to zero. It must not print as
		// "-0.00", which reads like a bug rather than like nothing.
		{"negative rounding to zero has no sign", -400, "0.00"},
		{"large", 1_234_567_890, "1234.57"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatEuro(tc.in); got != tc.want {
				t.Errorf("formatEuro(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatEuroMicro(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want string
	}{
		{"whole euros", 25_000_000, "25.000000"},
		// One usage tick is a fraction of a cent. Rounding it to cents would
		// print 0.00 and tell the reader nothing.
		{"sub-cent usage charge", -15_300, "-0.015300"},
		{"one micro-euro", 1, "0.000001"},
		{"rate", 7_500, "0.007500"},
		{"zero", 0, "0.000000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatEuroMicro(tc.in); got != tc.want {
				t.Errorf("formatEuroMicro(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The sign is what distinguishes a top-up from a usage charge at a glance, so
// a positive amount carries one explicitly.
func TestSignedEuroMicro(t *testing.T) {
	if got, want := signedEuroMicro(25_000_000), "+25.000000"; got != want {
		t.Errorf("signedEuroMicro = %q, want %q", got, want)
	}
	if got, want := signedEuroMicro(-15_300), "-0.015300"; got != want {
		t.Errorf("signedEuroMicro = %q, want %q", got, want)
	}
	if got, want := signedEuroMicro(0), "+0.000000"; got != want {
		t.Errorf("signedEuroMicro = %q, want %q", got, want)
	}
}

// Amounts are formatted from the integer micro-euro field, never from the
// float64 the API also sends: 0.01 has no exact binary representation and a
// balance is not a place to inherit rounding error.
func TestFormatEuroDoesNotGoThroughFloat(t *testing.T) {
	// 8.70 EUR: float64(8_700_000)/1e6 formats fine, but the sum that produces
	// it in float arithmetic (0.1+0.2 style) does not. The integer path is
	// exact by construction.
	if got, want := formatEuro(8_700_000), "8.70"; got != want {
		t.Errorf("formatEuro = %q, want %q", got, want)
	}
	if got, want := formatEuro(2_999_999), "3.00"; got != want {
		t.Errorf("formatEuro = %q, want %q", got, want)
	}
}

func TestNormalizeEuroAmount(t *testing.T) {
	ok := map[string]string{
		"25":      "25.00",
		"25.5":    "25.50",
		"25.50":   "25.50",
		"7.05":    "7.05",
		"  10  ":  "10.00",
		"0.01":    "0.01",
		"1000":    "1000.00",
		"1000.00": "1000.00",
	}
	for in, want := range ok {
		t.Run("accepts "+in, func(t *testing.T) {
			got, err := normalizeEuroAmount(in)
			if err != nil {
				t.Fatalf("normalizeEuroAmount(%q): %v", in, err)
			}
			if got != want {
				t.Errorf("normalizeEuroAmount(%q) = %q, want %q", in, got, want)
			}
		})
	}

	// Shape only. Bounds belong to hero-api and are published by
	// "heroctl pricing"; a limit hardcoded here would disagree with the server
	// the day it changes.
	bad := []string{"", "  ", "25.", "25.505", "-5", "abc", "2,50", ".50", "25 EUR", "€25", "1e3"}
	for _, in := range bad {
		t.Run("rejects "+in, func(t *testing.T) {
			if _, err := normalizeEuroAmount(in); err == nil {
				t.Errorf("normalizeEuroAmount(%q) should have been rejected", in)
			}
		})
	}
}

// An amount outside the limits is hero-api's call, not the CLI's — it must
// reach the server rather than being second-guessed locally.
func TestNormalizeEuroAmountDoesNotEnforceBounds(t *testing.T) {
	for _, in := range []string{"1", "0.01", "99999"} {
		if _, err := normalizeEuroAmount(in); err != nil {
			t.Errorf("normalizeEuroAmount(%q) = %v; bounds are the server's to enforce", in, err)
		}
	}
}

func TestBalanceCmd(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/orgs/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"ID": "org-1", "Name": "demo"})
		case "/api/v1/orgs/org-1/balance":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"balance_micro_eur": 12_500_000, "balance_eur": 12.5,
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	cmd := balanceCmd(newTestDeps(srv))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := out.String()
	for _, want := range []string{"demo", "org-1", "12.50 EUR"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q missing %q", got, want)
		}
	}
	if len(gotPaths) != 2 {
		t.Errorf("requested %v, want the org then its balance", gotPaths)
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
			// This is hero-api's exact wording since billing moved to euros.
			// The old match was on "credit", which no longer appears anywhere
			// in the response, so this case silently lost its hint.
			name:     "insufficient balance points at the balance",
			err:      errors.New("insufficient balance"),
			wantHint: "heroctl balance",
		},
		{
			name:     "balance wording varies and is matched case-insensitively",
			err:      errors.New("Not enough Balance for this deployment"),
			wantHint: "heroctl balance",
		},
		{
			name:     "the hint names how to fix it, not just where to look",
			err:      errors.New("insufficient balance"),
			wantHint: "heroctl balance topup",
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

func TestBalanceLedger_ShowsSignAndReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/balance/ledger" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{"id": "1", "delta_micro_eur": 25_000_000, "delta_eur": 25.0,
					"reason": "mollie payment tr_1", "created_at": "2026-08-24T10:00:00Z"},
				{"id": "2", "delta_micro_eur": -15_300, "delta_eur": -0.0153,
					"reason":     "usage: 1 vCPU, 1024 MB RAM, 0 GB storage for 5m0s",
					"created_at": "2026-08-24T10:05:00Z"},
			},
			"total": 2, "limit": 20, "offset": 0,
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, balanceLedgerCmd)

	if !strings.Contains(out, "+25.000000") {
		t.Errorf("a top-up must show a leading +, got:\n%s", out)
	}
	// A usage tick is a fraction of a cent. Shown at cent precision it would
	// read as 0.00 and the ledger would look empty of charges.
	if !strings.Contains(out, "-0.015300") {
		t.Errorf("a sub-cent usage charge must keep its precision, got:\n%s", out)
	}
	if !strings.Contains(out, "mollie payment tr_1") {
		t.Errorf("the reason must be shown, got:\n%s", out)
	}
}

func TestBalanceLedger_PassesPagination(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{}, "total": 0})
	}))
	defer srv.Close()

	runCmd(t, srv, balanceLedgerCmd, "--limit", "5", "--offset", "10")

	if !strings.Contains(gotQuery, "limit=5") || !strings.Contains(gotQuery, "offset=10") {
		t.Errorf("pagination not forwarded, query was %q", gotQuery)
	}
}

func TestBalanceLedger_EmptyIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{}, "total": 0})
	}))
	defer srv.Close()

	out := runCmd(t, srv, balanceLedgerCmd)

	if !strings.Contains(out, "No ledger entries") {
		t.Errorf("an empty ledger should say so, got:\n%s", out)
	}
}

func TestBalanceLedger_HintsAtTheNextPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{{"id": "1", "delta_micro_eur": 1_000_000, "reason": "x",
				"created_at": "2026-08-24T10:00:00Z"}},
			"total": 50, "limit": 1, "offset": 0,
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, balanceLedgerCmd)

	// Without this a user sees 1 of 50 rows and no way to know more exist.
	if !strings.Contains(out, "--offset 1") {
		t.Errorf("a truncated ledger must point at the next page, got:\n%s", out)
	}
}

func TestPricing_ShowsRatesAndTopUpBounds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pricing" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"vcpu_hour_micro_eur":    7_500,
			"gb_ram_hour_micro_eur":  6_000,
			"gb_disk_hour_micro_eur": 200,
			"min_topup_micro_eur":    5_000_000,
			"max_topup_micro_eur":    1_000_000_000,
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, pricingCmd)

	// Rates are sub-cent, so they must not be rounded to cents.
	for _, want := range []string{"0.007500", "0.006000", "0.000200"} {
		if !strings.Contains(out, want) {
			t.Errorf("rate %s missing, got:\n%s", want, out)
		}
	}
	// The bounds are the reason this command replaced "credits packages":
	// without them there is no way to learn what a top-up may be.
	if !strings.Contains(out, "5.00") || !strings.Contains(out, "1000.00") {
		t.Errorf("top-up bounds missing, got:\n%s", out)
	}
}

func TestBalanceTopup_SendsTheAmountAndPrintsTheCheckoutURL(t *testing.T) {
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"payment_id": "tr_1", "checkout_url": "https://mollie.test/tr_1",
			"amount_eur": "25.00",
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, balanceTopupCmd, "25")

	// Normalised to two decimals before sending, so "25" is not rejected
	// upstream and the amount charged is unambiguous.
	if body["amount_eur"] != "25.00" {
		t.Errorf("amount not forwarded as a two-decimal string, got %v", body)
	}
	if _, stale := body["package"]; stale {
		t.Errorf("the package field is gone from hero-api and must not be sent, got %v", body)
	}
	if !strings.Contains(out, "https://mollie.test/tr_1") {
		t.Errorf("the checkout URL is the whole point of the command, got:\n%s", out)
	}
}

func TestBalanceTopup_RejectsAMalformedAmountWithoutCallingTheAPI(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	cmd := balanceTopupCmd(newTestDeps(srv))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"25.505"})

	if err := cmd.Execute(); err == nil {
		t.Error("an amount with three decimals should be rejected")
	}
	if called {
		t.Error("a malformed amount must not cost a round trip")
	}
}

func TestBalanceTopup_MissingBillingProfileIsActionable(t *testing.T) {
	// hero-api answers 428 when no billing profile exists. "checkout failed"
	// would leave the user guessing at something one command fixes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionRequired)
		_, _ = w.Write([]byte(`{"error":"a billing profile is required before topping up"}`))
	}))
	defer srv.Close()

	cmd := balanceTopupCmd(newTestDeps(srv))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"25.00"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for a missing billing profile")
	}
	if !strings.Contains(err.Error(), "heroctl billing set") {
		t.Errorf("the error must name the command that fixes it, got: %v", err)
	}
}

func TestBalancePayments_ListsHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"payments": []map[string]any{
				{"id": "u1", "mollie_id": "tr_1", "amount_eur": "25.00",
					"status": "paid", "invoice_number": "2026-0001",
					"created_at": "2026-08-24T10:00:00Z"},
				// Invoicing is best-effort inside hero-api's webhook, so a
				// recent payment legitimately has no invoice number yet.
				{"id": "u2", "mollie_id": "tr_2", "amount_eur": "10.00",
					"status": "open", "created_at": "2026-08-25T10:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, balancePaymentsCmd)

	for _, want := range []string{"tr_1", "paid", "25.00", "2026-0001"} {
		if !strings.Contains(out, want) {
			t.Errorf("payment history missing %q, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "-") {
		t.Errorf("a payment without an invoice should show a dash, got:\n%s", out)
	}
}

// A refund or chargeback leaves Mollie's own status on "paid"; hero-api reports
// "refunded" instead. Passing it through unchanged is the whole point — a
// customer whose balance was clawed back must not see a plain success.
func TestBalancePayments_ShowsRefundedStatusAsSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"payments": []map[string]any{
				{"id": "u1", "mollie_id": "tr_1", "amount_eur": "25.00",
					"status": "refunded", "created_at": "2026-08-24T10:00:00Z"},
			},
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, balancePaymentsCmd)

	if !strings.Contains(out, "refunded") {
		t.Errorf("a reversed payment must not read as a success, got:\n%s", out)
	}
}

func TestBalancePayments_EmptyIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"payments": []any{}})
	}))
	defer srv.Close()

	if out := runCmd(t, srv, balancePaymentsCmd); !strings.Contains(out, "No payments yet") {
		t.Errorf("an empty history should say so, got:\n%s", out)
	}
}
