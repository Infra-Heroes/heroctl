package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// logsTestServer serves the project lookup plus a log stream with the given body.
func logsTestServer(t *testing.T, logBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/projects" {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "proj-1", "name": "demo"},
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/logs") {
			_, _ = w.Write([]byte(logBody))
			return
		}
		t.Errorf("unexpected request path %q", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
}

func runLogs(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	var out bytes.Buffer
	cmd := logsCmd(newTestDeps(srv))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"my-app", "--project", "demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return out.String()
}

func TestLogsStreamsOrdinaryLines(t *testing.T) {
	srv := logsTestServer(t, "first\nsecond\nthird\n")
	defer srv.Close()

	if got, want := runLogs(t, srv), "first\nsecond\nthird\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// Regression: the previous bufio.Scanner implementation capped a token at 64KB
// and returned ErrTooLong on a longer line, which aborted the whole stream —
// the oversized line AND every line after it were dropped.
func TestLogsHandlesLineOver64KB(t *testing.T) {
	long := strings.Repeat("x", 70000)
	srv := logsTestServer(t, long+"\nafter-the-long-line\n")
	defer srv.Close()

	out := runLogs(t, srv)

	if !strings.Contains(out, long) {
		t.Errorf("oversized line was dropped (got %d bytes of output)", len(out))
	}
	if !strings.Contains(out, "after-the-long-line") {
		t.Error("stream aborted: the line following the oversized one was dropped")
	}
	if want := len(long) + len("\nafter-the-long-line\n"); len(out) != want {
		t.Errorf("output length = %d, want %d", len(out), want)
	}
}

// A final line without a trailing newline must not be swallowed.
func TestLogsPreservesUnterminatedFinalLine(t *testing.T) {
	srv := logsTestServer(t, "line-one\nno-trailing-newline")
	defer srv.Close()

	if got, want := runLogs(t, srv), "line-one\nno-trailing-newline"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestLogsHistoryPassesTheWindowAndPrintsLinesWithTime(t *testing.T) {
	// The lines are shown in the reader's time zone, like "jobs runs"; the test
	// pins it so the expected text does not depend on the machine.
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "proj-1", "name": "demo"}})
		case strings.HasSuffix(r.URL.Path, "/logs/history"):
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"from":"2026-11-16T10:00:00Z","to":"2026-11-16T12:00:00Z","runs":2,"truncated":true,
			 "lines":[{"time":"2026-11-16T10:30:01Z","stream":"stdout","alloc":"a","line":"boot\n"},
			          {"time":"2026-11-16T10:30:02Z","stream":"stderr","alloc":"b","line":"panic: x"}]}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	var out, errOut bytes.Buffer
	cmd := logsCmd(newTestDeps(srv))
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"my-app", "--project", "demo", "--since", "2h", "--grep", "panic|boot", "--tail", "50"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"since=2h", "limit=50", "q=panic%7Cboot"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q lacks %q", gotQuery, want)
		}
	}
	if want := "2026-11-16 10:30:01  boot\n2026-11-16 10:30:02  panic: x\n"; out.String() != want {
		t.Errorf("output = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "more lines exist") {
		t.Errorf("a truncated window must say so: %q", errOut.String())
	}
}

func TestLogsHistoryCannotFollow(t *testing.T) {
	srv := logsTestServer(t, "")
	defer srv.Close()
	cmd := logsCmd(newTestDeps(srv))
	cmd.SetArgs([]string{"my-app", "--project", "demo", "--since", "1h", "-f"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--follow") {
		t.Errorf("err = %v", err)
	}
}
