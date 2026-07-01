package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/auth"
)

func newTestClient(baseURL string) *Client {
	tok := &auth.Token{
		AccessToken: "test-access-token",
		Expiry:      time.Now().Add(1 * time.Hour), // not expired, so ensureToken never refreshes/saves
	}
	return New(baseURL, "auth.example.com", "test-client-id", tok)
}

func TestClient_GetOrg_Success(t *testing.T) {
	var gotAuthHeader, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Org{ID: "org-1", Name: "acme"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	org, err := c.GetOrg(context.Background())
	if err != nil {
		t.Fatalf("GetOrg() error = %v", err)
	}
	if org.ID != "org-1" || org.Name != "acme" {
		t.Errorf("GetOrg() = %+v, want ID=org-1 Name=acme", org)
	}
	if gotAuthHeader != "Bearer test-access-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuthHeader, "Bearer test-access-token")
	}
	if gotPath != "/api/v1/orgs/me" {
		t.Errorf("request path = %q, want %q", gotPath, "/api/v1/orgs/me")
	}
}

func TestClient_GetOrg_ServerErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "org access denied"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.GetOrg(context.Background())
	if err == nil {
		t.Fatal("GetOrg() expected an error, got nil")
	}
	if err.Error() != "org access denied" {
		t.Errorf("GetOrg() error = %q, want the server's error message %q", err.Error(), "org access denied")
	}
}

func TestClient_GetOrg_ServerErrorWithoutBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.GetOrg(context.Background())
	if err == nil {
		t.Fatal("GetOrg() expected an error, got nil")
	}
	if err.Error() != "server returned HTTP 500" {
		t.Errorf("GetOrg() error = %q, want fallback message for HTTP 500", err.Error())
	}
}

func TestClient_CreateProject_SendsJSONBody(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(Project{ID: "proj-1", Name: gotBody["name"]})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	proj, err := c.CreateProject(context.Background(), "my-app")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if gotBody["name"] != "my-app" {
		t.Errorf("request body name = %q, want %q", gotBody["name"], "my-app")
	}
	if proj.Name != "my-app" {
		t.Errorf("CreateProject() returned Name = %q, want %q", proj.Name, "my-app")
	}
}

func TestClient_ListProjects_NilBecomesEmptySlice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("null"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	projects, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if projects == nil {
		t.Error("ListProjects() = nil, want a non-nil empty slice")
	}
	if len(projects) != 0 {
		t.Errorf("ListProjects() = %v, want empty", projects)
	}
}
