package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// scheduleServer fakes the schedule endpoint and records what it was sent.
type scheduleServer struct {
	current map[string]any
	puts    []map[string]any
	deletes int
}

func newScheduleServer(t *testing.T, s *scheduleServer) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "app"}})
		case strings.HasSuffix(r.URL.Path, "/deployments/web/schedule") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(s.current)
		case strings.HasSuffix(r.URL.Path, "/deployments/web/schedule") && r.Method == http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.puts = append(s.puts, body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"enabled": body["enabled"], "start_cron": body["start_cron"], "stop_cron": body["stop_cron"], "timezone": body["timezone"],
			})
		case strings.HasSuffix(r.URL.Path, "/deployments/web/schedule") && r.Method == http.MethodDelete:
			s.deletes++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
}

func runSchedule(t *testing.T, srv *httptest.Server, args ...string) error {
	t.Helper()
	cmd := deploymentsScheduleCmd(newTestDeps(srv))
	cmd.SetArgs(append([]string{"web", "--project", "app"}, args...))
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	return cmd.Execute()
}

func TestSchedule_ShowingChangesNothing(t *testing.T) {
	s := &scheduleServer{current: map[string]any{"enabled": true, "stop_cron": "0 20 * * *", "start_cron": "0 7 * * *", "timezone": "UTC"}}
	srv := newScheduleServer(t, s)
	defer srv.Close()
	if err := runSchedule(t, srv); err != nil {
		t.Fatal(err)
	}
	if len(s.puts) != 0 || s.deletes != 0 {
		t.Errorf("showing a schedule must not write: puts=%v deletes=%d", s.puts, s.deletes)
	}
}

// The API replaces the whole timetable, so changing one side must send the other
// as it was, or a one-flag change would silently wipe the rest.
func TestSchedule_ChangingOneSideKeepsTheOther(t *testing.T) {
	s := &scheduleServer{current: map[string]any{"enabled": true, "stop_cron": "0 20 * * *", "start_cron": "0 7 * * *", "timezone": "Europe/Berlin"}}
	srv := newScheduleServer(t, s)
	defer srv.Close()

	if err := runSchedule(t, srv, "--stop", "0 22 * * *"); err != nil {
		t.Fatal(err)
	}
	got := s.puts[0]
	if got["stop_cron"] != "0 22 * * *" || got["start_cron"] != "0 7 * * *" || got["timezone"] != "Europe/Berlin" {
		t.Errorf("sent %v, want the new stop with the old start and zone", got)
	}
	if got["enabled"] != true {
		t.Errorf("setting a time means wanting it to run, enabled = %v", got["enabled"])
	}
}

func TestSchedule_OffPausesAndKeepsTheTimes(t *testing.T) {
	s := &scheduleServer{current: map[string]any{"enabled": true, "stop_cron": "0 20 * * *", "start_cron": "0 7 * * *", "timezone": "UTC"}}
	srv := newScheduleServer(t, s)
	defer srv.Close()

	if err := runSchedule(t, srv, "--off"); err != nil {
		t.Fatal(err)
	}
	got := s.puts[0]
	if got["enabled"] != false || got["stop_cron"] != "0 20 * * *" || got["start_cron"] != "0 7 * * *" {
		t.Errorf("sent %v, want paused with the times kept", got)
	}
}

func TestSchedule_OnResumes(t *testing.T) {
	s := &scheduleServer{current: map[string]any{"enabled": false, "stop_cron": "0 20 * * *", "start_cron": "0 7 * * *", "timezone": "UTC"}}
	srv := newScheduleServer(t, s)
	defer srv.Close()
	if err := runSchedule(t, srv, "--on"); err != nil {
		t.Fatal(err)
	}
	if s.puts[0]["enabled"] != true {
		t.Errorf("enabled = %v, want true", s.puts[0]["enabled"])
	}
}

func TestSchedule_RemoveDeletes(t *testing.T) {
	s := &scheduleServer{current: map[string]any{}}
	srv := newScheduleServer(t, s)
	defer srv.Close()
	if err := runSchedule(t, srv, "--remove"); err != nil {
		t.Fatal(err)
	}
	if s.deletes != 1 || len(s.puts) != 0 {
		t.Errorf("deletes=%d puts=%d, want one delete and no put", s.deletes, len(s.puts))
	}
}

func TestSchedule_Contradictions(t *testing.T) {
	s := &scheduleServer{current: map[string]any{}}
	srv := newScheduleServer(t, s)
	defer srv.Close()
	for _, args := range [][]string{
		{"--on", "--off"},
		{"--remove", "--stop", "0 20 * * *"},
		{"--remove", "--off"},
	} {
		if err := runSchedule(t, srv, args...); err == nil {
			t.Errorf("%v must be refused", args)
		}
	}
	if len(s.puts) != 0 || s.deletes != 0 {
		t.Error("a refused combination must not reach the server")
	}
}
