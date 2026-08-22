package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
// and returned ErrTooLong on a longer line, which aborted the whole stream M-bM-^@M-^T
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
