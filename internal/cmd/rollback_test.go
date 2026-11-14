package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

func revs() []client.DeploymentRevision {
	return []client.DeploymentRevision{
		// Time-ordered ids: the start is the same, the end differs.
		{ID: "01a1d9e6-aaaa-7000-8000-00000000a1b2", Image: "api.example/org/proj:3", CPU: 1, MemoryMB: 256, Replicas: 1, Current: true},
		{ID: "01a1d9e6-bbbb-7000-8000-00000000c3d4", Image: "api.example/org/proj:2", CPU: 1, MemoryMB: 256, Replicas: 1},
		{ID: "01a1d9e6-cccc-7000-8000-00000000e5d4", Image: "api.example/org/proj:1", CPU: 2, MemoryMB: 512, Replicas: 2},
	}
}

func TestPickRevision(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		revs       []client.DeploymentRevision
		wantID     string
		wantErr    string
	}{
		{"no name takes the one before the newest", "", revs(), "01a1d9e6-bbbb-7000-8000-00000000c3d4", ""},
		{"no name with a single revision", "", revs()[:1], "", "no earlier revision"},
		{"no name with no revision", "", nil, "", "no earlier revision"},
		{"unique end", "a1b2", revs(), "01a1d9e6-aaaa-7000-8000-00000000a1b2", ""},
		{"the eight characters the list shows", "0000a1b2", revs(), "01a1d9e6-aaaa-7000-8000-00000000a1b2", ""},
		{"end is case-insensitive", "A1B2", revs(), "01a1d9e6-aaaa-7000-8000-00000000a1b2", ""},
		{"the whole id", "01a1d9e6-bbbb-7000-8000-00000000c3d4", revs(), "01a1d9e6-bbbb-7000-8000-00000000c3d4", ""},
		{"longer end separates two that share a tail", "c3d4", revs(), "01a1d9e6-bbbb-7000-8000-00000000c3d4", ""},
		{"the start is the same for all, so it is not a name", "01a1", revs(), "", "no revision ends with"},
		{"ambiguous end", "beef", []client.DeploymentRevision{
			{ID: "01a1d9e6-aaaa-7000-8000-00000000beef"},
			{ID: "01a1d9e6-bbbb-7000-8000-00000001beef"},
			{ID: "01a1d9e6-cccc-7000-8000-00000000a1b2"},
		}, "", "2 revisions end with"},
		{"an end shared with nothing is unknown", "0000", revs(), "", "no revision ends with"},
		{"unknown end", "zzzz", revs(), "", "no revision ends with"},
		{"too short", "d4", revs(), "", "too short"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickRevision(tc.revs, tc.want)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != tc.wantID {
				t.Errorf("picked %s, want %s", got.ID, tc.wantID)
			}
		})
	}
}

func TestShortImageAndID(t *testing.T) {
	if got := shortImage("api.infra-heroes.de/org/proj:42"); got != "proj:42" {
		t.Errorf("shortImage = %q", got)
	}
	if got := shortImage("nginx:1.31"); got != "nginx:1.31" {
		t.Errorf("an image without a registry must stay whole, got %q", got)
	}
	if got := shortID("01a1d9e6-aaaa-7000-8000-00000000a1b2"); got != "0000a1b2" {
		t.Errorf("shortID = %q", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Errorf("a short id must stay whole, got %q", got)
	}
}

type revisionServer struct {
	calls    []string
	revs     []client.DeploymentRevision
	rollback int // status to answer the rollback with; 0 means 200
}

func newRevisionServer(t *testing.T, s *revisionServer) *httptest.Server {
	t.Helper()
	if s.revs == nil {
		s.revs = revs()
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		s.calls = append(s.calls, key)
		switch {
		case key == "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case key == "GET /api/v1/projects/proj-1/deployments/web/revisions":
			_ = json.NewEncoder(w).Encode(map[string]any{"revisions": s.revs, "total": len(s.revs)})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollback"):
			if s.rollback != 0 {
				w.WriteHeader(s.rollback)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "insufficient balance"})
				return
			}
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/projects/proj-1/deployments/web/revisions/"), "/rollback")
			_ = json.NewEncoder(w).Encode(map[string]any{"app_name": "web", "image": "api.example/org/proj:2", "status": "pending", "rolled_to": id})
		default:
			http.NotFound(w, r)
		}
	}))
}

func runRevisions(t *testing.T, srv *httptest.Server, stdin string, args ...string) error {
	t.Helper()
	deps := newTestDeps(srv)
	cmd := deploymentsRollbackCmd(deps)
	if len(args) > 0 && args[0] == "revisions" {
		cmd = deploymentsRevisionsCmd(deps)
		args = args[1:]
	}
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd.Execute()
}

func rolledBack(calls []string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasSuffix(c, "/rollback") {
			out = append(out, c)
		}
	}
	return out
}

func TestRevisionsList(t *testing.T) {
	s := &revisionServer{}
	srv := newRevisionServer(t, s)
	defer srv.Close()
	if err := runRevisions(t, srv, "", "revisions", "web", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	if len(rolledBack(s.calls)) != 0 {
		t.Error("listing must not roll anything back")
	}
}

func TestRevisionsList_NoRevisions(t *testing.T) {
	s := &revisionServer{revs: []client.DeploymentRevision{}}
	srv := newRevisionServer(t, s)
	defer srv.Close()
	if err := runRevisions(t, srv, "", "revisions", "web", "--project", "app"); err != nil {
		t.Fatalf("an empty history is not an error: %v", err)
	}
}

func TestRollback_DefaultGoesBackOne(t *testing.T) {
	s := &revisionServer{}
	srv := newRevisionServer(t, s)
	defer srv.Close()
	if err := runRevisions(t, srv, "", "web", "--project", "app", "--force"); err != nil {
		t.Fatal(err)
	}
	got := rolledBack(s.calls)
	want := "POST /api/v1/projects/proj-1/deployments/web/revisions/01a1d9e6-bbbb-7000-8000-00000000c3d4/rollback"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %v, want exactly %s", got, want)
	}
}

func TestRollback_ByIDEnd(t *testing.T) {
	s := &revisionServer{}
	srv := newRevisionServer(t, s)
	defer srv.Close()
	if err := runRevisions(t, srv, "", "web", "e5d4", "--project", "app", "--force"); err != nil {
		t.Fatal(err)
	}
	if got := rolledBack(s.calls); len(got) != 1 || !strings.Contains(got[0], "/01a1d9e6-cccc-7000-8000-00000000e5d4/rollback") {
		t.Fatalf("calls = %v, want the revision ending e5d4", got)
	}
}

func TestRollback_AskingFirstAndDecliningSendsNothing(t *testing.T) {
	s := &revisionServer{}
	srv := newRevisionServer(t, s)
	defer srv.Close()
	if err := runRevisions(t, srv, "n\n", "web", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	if got := rolledBack(s.calls); len(got) != 0 {
		t.Fatalf("declined, yet sent %v", got)
	}
}

func TestRollback_AskingFirstAndConfirmingSendsIt(t *testing.T) {
	s := &revisionServer{}
	srv := newRevisionServer(t, s)
	defer srv.Close()
	if err := runRevisions(t, srv, "y\n", "web", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	if got := rolledBack(s.calls); len(got) != 1 {
		t.Fatalf("confirmed, but calls = %v", got)
	}
}

func TestRollback_AmbiguousOrUnknownSendsNothing(t *testing.T) {
	for _, want := range []string{"0000", "zzzz", "d4"} {
		s := &revisionServer{}
		srv := newRevisionServer(t, s)
		if err := runRevisions(t, srv, "", "web", want, "--project", "app", "--force"); err == nil {
			t.Errorf("%q: expected an error", want)
		}
		if got := rolledBack(s.calls); len(got) != 0 {
			t.Errorf("%q: sent %v although the revision was not clear", want, got)
		}
		srv.Close()
	}
}

func TestRollback_OnlyOneRevisionHasNothingToGoBackTo(t *testing.T) {
	s := &revisionServer{revs: revs()[:1]}
	srv := newRevisionServer(t, s)
	defer srv.Close()
	err := runRevisions(t, srv, "", "web", "--project", "app", "--force")
	if err == nil || !strings.Contains(err.Error(), "no earlier revision") {
		t.Fatalf("err = %v", err)
	}
}

func TestRollback_ServerRefusalIsShown(t *testing.T) {
	s := &revisionServer{rollback: http.StatusPaymentRequired}
	srv := newRevisionServer(t, s)
	defer srv.Close()
	err := runRevisions(t, srv, "", "web", "--project", "app", "--force")
	if err == nil || !strings.Contains(err.Error(), "insufficient balance") {
		t.Fatalf("err = %v, want the server's reason", err)
	}
}
