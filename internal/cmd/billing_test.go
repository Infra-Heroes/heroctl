package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBilling_ShowsProfileAndVAT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/billing/profile" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"recipient_type": "business", "name": "Acme GmbH",
			"email": "billing@acme.example", "street": "Teststr. 1",
			"postal_code": "10115", "city": "Berlin", "country": "DE",
			"vat_number": "DE123456789", "vat_rate": "19.00",
			"vat_reverse_charge": false,
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, billingCmd)

	for _, want := range []string{"Acme GmbH", "Berlin", "DE123456789", "19.00"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestBilling_ReverseChargeIsStatedNotJustZero(t *testing.T) {
	// "0%" alone looks like a bug. The invoice must say why, and so must this.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"recipient_type": "business", "name": "Acme SARL", "country": "FR",
			"vat_number": "FR12345678901", "vat_rate": "0.00",
			"vat_reverse_charge": true,
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, billingCmd)

	if !strings.Contains(strings.ToLower(out), "reverse charge") {
		t.Errorf("reverse charge must be named, got:\n%s", out)
	}
}

func TestBilling_MissingProfileIsGuidanceNotAnError(t *testing.T) {
	// No profile is the normal state before a first purchase.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no billing profile set"}`))
	}))
	defer srv.Close()

	out := runCmd(t, srv, billingCmd)

	if !strings.Contains(out, "heroctl billing set") {
		t.Errorf("must point at the command that fixes it, got:\n%s", out)
	}
}

func TestBillingSet_SendsTheProfile(t *testing.T) {
	var got map[string]any
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"recipient_type": "business", "country": "FR",
			"vat_rate": "0.00", "vat_reverse_charge": true,
		})
	}))
	defer srv.Close()

	out := runCmd(t, srv, billingSetCmd,
		"--type", "business", "--name", "Acme SARL", "--email", "b@acme.example",
		"--street", "1 Rue", "--postal-code", "75001", "--city", "Paris",
		"--country", "fr", "--vat-number", "fr12345678901")

	if gotMethod != http.MethodPut {
		t.Errorf("expected PUT, got %s", gotMethod)
	}
	// Normalised before sending, so a lowercase entry is not rejected upstream.
	if got["country"] != "FR" {
		t.Errorf("country should be upper-cased, got %v", got["country"])
	}
	if got["vat_number"] != "FR12345678901" {
		t.Errorf("VAT number should be upper-cased, got %v", got["vat_number"])
	}
	if !strings.Contains(strings.ToLower(out), "reverse charge") {
		t.Errorf("the resulting VAT treatment should be confirmed, got:\n%s", out)
	}
}

func TestBillingSet_ValidatesBeforeCalling(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	for name, args := range map[string][]string{
		"bad type":       {"--type", "company", "--name", "x", "--email", "a@b.c", "--street", "s", "--postal-code", "1", "--city", "c", "--country", "DE"},
		"bad country":    {"--type", "consumer", "--name", "x", "--email", "a@b.c", "--street", "s", "--postal-code", "1", "--city", "c", "--country", "Germany"},
		"missing name":   {"--type", "consumer", "--email", "a@b.c", "--street", "s", "--postal-code", "1", "--city", "c", "--country", "DE"},
		"missing street": {"--type", "consumer", "--name", "x", "--email", "a@b.c", "--postal-code", "1", "--city", "c", "--country", "DE"},
	} {
		t.Run(name, func(t *testing.T) {
			cmd := billingSetCmd(newTestDeps(srv))
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(args)
			if err := cmd.Execute(); err == nil {
				t.Errorf("%s should be rejected", name)
			}
		})
	}
	if called {
		t.Error("an invalid profile must never reach hero-api")
	}
}

func TestCredits_GraceWindowIsSurfaced(t *testing.T) {
	// The grace window exists to warn. If nothing shows it, the customer only
	// learns about it when their deployments stop.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/credits") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"credits": -1.5, "milli_credits": -1500,
				"grace_until": "2026-08-25T12:00:00Z",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ID": "org-1", "Name": "acme", "VmCap": 2})
	}))
	defer srv.Close()

	out := runCmd(t, srv, creditsCmd)

	if !strings.Contains(out, "Out of credits") {
		t.Errorf("the grace window must be announced, got:\n%s", out)
	}
	if !strings.Contains(out, "credits topup") {
		t.Errorf("it must say how to fix it, got:\n%s", out)
	}
}

func TestCredits_NoGraceNoWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/credits") {
			_ = json.NewEncoder(w).Encode(map[string]any{"credits": 42.0, "milli_credits": 42000})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ID": "org-1", "Name": "acme", "VmCap": 2})
	}))
	defer srv.Close()

	out := runCmd(t, srv, creditsCmd)

	if strings.Contains(out, "Out of credits") {
		t.Errorf("a healthy balance must not warn, got:\n%s", out)
	}
	if !strings.Contains(out, "42.000") {
		t.Errorf("balance missing, got:\n%s", out)
	}
}
