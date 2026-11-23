package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// authServer stands in for the identity provider. The endpoints are https, so
// the test swaps the default client for the server's own and points authDomain at it.
func authServer(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	old := http.DefaultClient
	http.DefaultClient = srv.Client()
	t.Cleanup(func() { http.DefaultClient = old; srv.Close() })
	return strings.TrimPrefix(srv.URL, "https://")
}

func TestToken_ExpiryHasABuffer(t *testing.T) {
	if (&Token{Expiry: time.Now().Add(time.Hour)}).IsExpired() {
		t.Error("an hour left is not expired")
	}
	if !(&Token{Expiry: time.Now().Add(10 * time.Second)}).IsExpired() {
		t.Error("ten seconds left must count as expired: it could end mid-request")
	}
}

func TestTokenFile_RoundTripIsPrivateAndErrorsAreHelpful(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "heroctl login") {
		t.Errorf("no file: %v", err)
	}
	want := &Token{AccessToken: "a", RefreshToken: "r", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour).Truncate(time.Second)}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || got.AccessToken != "a" || got.RefreshToken != "r" || !got.Expiry.Equal(want.Expiry) {
		t.Fatalf("got %+v err %v", got, err)
	}
	p, _ := tokenPath()
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("the token file is %v; it must be private", fi.Mode().Perm())
	}
	if di, _ := os.Stat(filepath.Dir(p)); di.Mode().Perm() != 0o700 {
		t.Errorf("the directory is %v", di.Mode().Perm())
	}

	if err := os.WriteFile(p, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("malformed: %v", err)
	}
	if err := os.WriteFile(p, []byte(`{"access_token":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty: %v", err)
	}
}

func TestStartDeviceFlow(t *testing.T) {
	var form url.Values
	domain := authServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc", "user_code": "ABCD-EFGH", "verification_uri": "https://x/device", "interval": 5})
	})
	got, err := StartDeviceFlow(context.Background(), domain, "client-1")
	if err != nil || got.DeviceCode != "dc" || got.UserCode != "ABCD-EFGH" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if form.Get("client_id") != "client-1" || !strings.Contains(form.Get("scope"), "offline_access") {
		t.Errorf("request form = %v", form)
	}
}

func TestStartDeviceFlow_Failures(t *testing.T) {
	bad := authServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) })
	if _, err := StartDeviceFlow(context.Background(), bad, "wrong"); err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("a rejected client id: %v", err)
	}
	empty := authServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	if _, err := StartDeviceFlow(context.Background(), empty, "c"); err == nil || !strings.Contains(err.Error(), "empty device_code") {
		t.Errorf("no device code: %v", err)
	}
}

func TestPollToken_WaitsThenReturnsTheToken(t *testing.T) {
	calls := 0
	domain := authServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 2 {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600, "token_type": "Bearer"})
	})
	tok, err := PollToken(context.Background(), domain, "c", "dc", 1)
	if err != nil || tok.AccessToken != "at" || tok.RefreshToken != "rt" || tok.IsExpired() {
		t.Fatalf("tok %+v err %v", tok, err)
	}
	if calls != 2 {
		t.Errorf("polled %d times", calls)
	}
}

func TestPollToken_TerminalErrors(t *testing.T) {
	for code, want := range map[string]string{
		"expired_token": "expired",
		"access_denied": "access denied",
		"server_error":  `token error "server_error"`,
	} {
		domain := authServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
		})
		if _, err := PollToken(context.Background(), domain, "c", "dc", 1); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", code, err, want)
		}
	}
}

func TestPollToken_StopsWhenCancelled(t *testing.T) {
	domain := authServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := PollToken(ctx, domain, "c", "dc", 1); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("err = %v", err)
	}
}

func TestRefresh(t *testing.T) {
	var form url.Values
	domain := authServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new", "refresh_token": "r2", "expires_in": 60})
	})
	tok, err := Refresh(context.Background(), domain, "c", "old-refresh")
	if err != nil || tok.AccessToken != "new" || tok.RefreshToken != "r2" {
		t.Fatalf("tok %+v err %v", tok, err)
	}
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "old-refresh" {
		t.Errorf("form = %v", form)
	}

	rejected := authServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "revoked"})
	})
	if _, err := Refresh(context.Background(), rejected, "c", "x"); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Errorf("a revoked refresh token: %v", err)
	}
}

func TestRefreshWithoutBuildSettingsSaysWhy(t *testing.T) {
	for _, c := range []struct{ domain, client string }{{"", "c"}, {"auth.example.com", ""}} {
		_, err := Refresh(context.Background(), c.domain, c.client, "r")
		if !errors.Is(err, ErrUnconfiguredBuild) {
			t.Errorf("Refresh(%q, %q) = %v, want ErrUnconfiguredBuild", c.domain, c.client, err)
		}
	}
}
