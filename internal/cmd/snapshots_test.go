package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// snapshotServer fakes the part of hero-api the snapshot commands use, and
// records what it was asked.
type snapshotServer struct {
	calls  []string
	bodies map[string]map[string]any
	policy map[string]any
	status string // volume status reported by the list
}

func newSnapshotServer(t *testing.T, s *snapshotServer) *httptest.Server {
	t.Helper()
	s.bodies = map[string]map[string]any{}
	if s.status == "" {
		s.status = "available"
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		s.calls = append(s.calls, key)
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			s.bodies[key] = m
		}
		switch {
		case key == "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case key == "GET /api/v1/projects/proj-1/volumes":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "vol-1", "name": "data", "size_gb": 10, "status": s.status}})
		case key == "GET /api/v1/projects/proj-1/volumes/vol-1/snapshots":
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshots": []map[string]any{
				{"name": "auto-20261111-020000", "size_bytes": 2147483648},
				{"name": "before-upgrade", "size_bytes": 1073741824},
			}})
		case key == "POST /api/v1/projects/proj-1/volumes/vol-1/snapshots":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "auto-20261111-030000"})
		case strings.HasSuffix(key, "/restore"):
			_ = json.NewEncoder(w).Encode(map[string]any{"restored": "x"})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case key == "GET /api/v1/projects/proj-1/volumes/vol-1/snapshot-policy":
			_ = json.NewEncoder(w).Encode(s.policy)
		case key == "PUT /api/v1/projects/proj-1/volumes/vol-1/snapshot-policy":
			_ = json.NewEncoder(w).Encode(map[string]any{"configured": true, "enabled": s.bodies[key]["enabled"], "hour_utc": 3, "keep": 10})
		default:
			http.NotFound(w, r)
		}
	}))
}

func run(t *testing.T, srv *httptest.Server, args ...string) error {
	t.Helper()
	cmd := volumesSnapshotsCmd(newTestDeps(srv))
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd.Execute()
}

func TestSnapshotsCreate(t *testing.T) {
	s := &snapshotServer{}
	srv := newSnapshotServer(t, s)
	defer srv.Close()

	if err := run(t, srv, "create", "data", "--project", "app", "--name", "before-upgrade"); err != nil {
		t.Fatal(err)
	}
	got := s.bodies["POST /api/v1/projects/proj-1/volumes/vol-1/snapshots"]
	if got["name"] != "before-upgrade" {
		t.Errorf("body = %v, want the given name", got)
	}
}

func TestSnapshotsCreate_NoNameLetsTheServerChoose(t *testing.T) {
	s := &snapshotServer{}
	srv := newSnapshotServer(t, s)
	defer srv.Close()

	if err := run(t, srv, "create", "data", "--project", "app"); err != nil {
		t.Fatal(err)
	}
	if _, has := s.bodies["POST /api/v1/projects/proj-1/volumes/vol-1/snapshots"]["name"]; has {
		t.Error("no --name must not send a name")
	}
}

func TestSnapshotsList(t *testing.T) {
	s := &snapshotServer{}
	srv := newSnapshotServer(t, s)
	defer srv.Close()
	if err := run(t, srv, "list", "data", "--project", "app"); err != nil {
		t.Fatal(err)
	}
}

// A stopped deployment keeps its volume "attached", so the volume's status says
// nothing about whether a restore is safe. The request goes to the server, which
// knows, and the CLI does not stand in its way.
func TestSnapshotsRestore_AttachedVolumeIsLeftToTheServer(t *testing.T) {
	s := &snapshotServer{status: "attached"}
	srv := newSnapshotServer(t, s)
	defer srv.Close()

	if err := run(t, srv, "restore", "data", "auto-20261111-020000", "--project", "app", "--force"); err != nil {
		t.Fatalf("an attached volume must still be sent to the server: %v", err)
	}
	sent := false
	for _, c := range s.calls {
		if strings.HasSuffix(c, "/restore") {
			sent = true
		}
	}
	if !sent {
		t.Error("the restore was not sent")
	}
}

func TestSnapshotsRestore_ForceSendsIt(t *testing.T) {
	s := &snapshotServer{}
	srv := newSnapshotServer(t, s)
	defer srv.Close()

	if err := run(t, srv, "restore", "data", "auto-20261111-020000", "--project", "app", "--force"); err != nil {
		t.Fatal(err)
	}
	want := "POST /api/v1/projects/proj-1/volumes/vol-1/snapshots/auto-20261111-020000/restore"
	if s.calls[len(s.calls)-1] != want {
		t.Errorf("last call = %q, want %q", s.calls[len(s.calls)-1], want)
	}
}

func TestSnapshotsDelete_Force(t *testing.T) {
	s := &snapshotServer{}
	srv := newSnapshotServer(t, s)
	defer srv.Close()
	if err := run(t, srv, "delete", "data", "before-upgrade", "--project", "app", "--force"); err != nil {
		t.Fatal(err)
	}
	want := "DELETE /api/v1/projects/proj-1/volumes/vol-1/snapshots/before-upgrade"
	if s.calls[len(s.calls)-1] != want {
		t.Errorf("last call = %q, want %q", s.calls[len(s.calls)-1], want)
	}
}

func TestSnapshotsSchedule(t *testing.T) {
	t.Run("without flags it only reads", func(t *testing.T) {
		s := &snapshotServer{policy: map[string]any{"configured": false, "enabled": false, "hour_utc": 2, "keep": 7}}
		srv := newSnapshotServer(t, s)
		defer srv.Close()
		if err := run(t, srv, "schedule", "data", "--project", "app"); err != nil {
			t.Fatal(err)
		}
		for _, c := range s.calls {
			if strings.HasPrefix(c, "PUT") {
				t.Error("showing the schedule must not change it")
			}
		}
	})

	t.Run("sends only what was given", func(t *testing.T) {
		s := &snapshotServer{}
		srv := newSnapshotServer(t, s)
		defer srv.Close()
		if err := run(t, srv, "schedule", "data", "--project", "app", "--at", "3"); err != nil {
			t.Fatal(err)
		}
		body := s.bodies["PUT /api/v1/projects/proj-1/volumes/vol-1/snapshot-policy"]
		if body["hour_utc"] != float64(3) || body["enabled"] != true {
			t.Errorf("body = %v", body)
		}
		if _, has := body["keep"]; has {
			t.Error("--keep was not given, so it must not be sent")
		}
	})

	t.Run("--off turns it off", func(t *testing.T) {
		s := &snapshotServer{}
		srv := newSnapshotServer(t, s)
		defer srv.Close()
		if err := run(t, srv, "schedule", "data", "--project", "app", "--off"); err != nil {
			t.Fatal(err)
		}
		if got := s.bodies["PUT /api/v1/projects/proj-1/volumes/vol-1/snapshot-policy"]["enabled"]; got != false {
			t.Errorf("enabled = %v, want false", got)
		}
	})

	t.Run("--off with --at is a contradiction", func(t *testing.T) {
		s := &snapshotServer{}
		srv := newSnapshotServer(t, s)
		defer srv.Close()
		if err := run(t, srv, "schedule", "data", "--project", "app", "--off", "--at", "3"); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestSnapshots_UnknownVolume(t *testing.T) {
	s := &snapshotServer{}
	srv := newSnapshotServer(t, s)
	defer srv.Close()
	err := run(t, srv, "list", "nope", "--project", "app")
	if err == nil || !strings.Contains(err.Error(), `volume "nope" not found`) {
		t.Fatalf("err = %v", err)
	}
}
