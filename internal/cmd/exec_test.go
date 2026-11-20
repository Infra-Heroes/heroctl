package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Infra-Heroes/heroctl/internal/client"
	"github.com/Infra-Heroes/heroctl/internal/exec"
	"github.com/Infra-Heroes/heroctl/internal/exec/exectest"
)

// execAPI serves the project list and the issuance endpoint. issue decides the answer.
func execAPI(t *testing.T, issue func(w http.ResponseWriter, body client.ExecSessionRequest)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "p1", "name": "shop"}})
		case "POST /api/v1/projects/p1/deployments/web/exec-sessions":
			var body client.ExecSessionRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			issue(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func issueFrom(t *testing.T, gw *exectest.Server) func(http.ResponseWriter, client.ExecSessionRequest) {
	return func(w http.ResponseWriter, body client.ExecSessionRequest) {
		sess, err := gw.Session(body.PublicKey)
		if err != nil {
			t.Error(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(sess)
	}
}

// syncBuffer lets stdout and stderr share one buffer, as the two copy goroutines write at once.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func runExec(t *testing.T, srv *httptest.Server, stdin string, args ...string) (string, error) {
	t.Helper()
	c := appExecCmd(newTestDeps(srv))
	var out syncBuffer
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&out)
	c.SilenceUsage, c.SilenceErrors = true, true
	c.SetArgs(args)
	err := c.ExecuteContext(context.Background())
	return out.String(), err
}

func TestExec_RunsTheCommandAndPassesTheExitCode(t *testing.T) {
	gw, closeGW := exectest.New()
	t.Cleanup(closeGW)
	gw.Run = func(argv []string, stdin io.Reader, stdout, stderr io.Writer) (uint32, string) {
		_, _ = io.Copy(stdout, stdin)
		_, _ = stderr.Write([]byte("[" + strings.Join(argv, "|") + "]"))
		return 5, ""
	}
	srv := execAPI(t, issueFrom(t, gw))
	out, err := runExec(t, srv, "piped", "web", "--project", "shop", "--", "echo", "it's", "--flag")
	var exit *ExitCodeError
	if !errors.As(err, &exit) || exit.Code != 5 {
		t.Fatalf("err = %v, want exit code 5", err)
	}
	if !strings.Contains(out, "piped") || !strings.Contains(out, "[echo|it's|--flag]") {
		t.Errorf("output = %q", out)
	}
}

func TestExec_SendsTheReplicaAndNoCredentialsToTheOutput(t *testing.T) {
	gw, closeGW := exectest.New()
	t.Cleanup(closeGW)
	gw.Run = func([]string, io.Reader, io.Writer, io.Writer) (uint32, string) { return 0, "" }
	var replica string
	srv := execAPI(t, func(w http.ResponseWriter, body client.ExecSessionRequest) {
		replica = body.Replica
		issueFrom(t, gw)(w, body)
	})
	out, err := runExec(t, srv, "", "web", "--project", "shop", "--replica", "alloc-2", "--", "true")
	if err != nil {
		t.Fatal(err)
	}
	if replica != "alloc-2" {
		t.Errorf("replica = %q", replica)
	}
	if strings.Contains(out, exectest.Ticket) || strings.Contains(out, "ssh-ed25519") {
		t.Errorf("output leaks credentials: %q", out)
	}
}

func TestExec_ConflictPrintsReplicasAndHint(t *testing.T) {
	srv := execAPI(t, func(w http.ResponseWriter, _ client.ExecSessionRequest) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "web has 2 running replicas", "replicas": []string{"a1b2", "c3d4"}})
	})
	_, err := runExec(t, srv, "", "web", "--project", "shop", "--", "true")
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"web has 2 running replicas", "a1b2", "c3d4", "--replica"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestExec_PATErrorIsPrintedAsTheServerSaidIt(t *testing.T) {
	const msg = `exec needs an interactive login: run "heroctl login"`
	srv := execAPI(t, func(w http.ResponseWriter, _ client.ExecSessionRequest) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	})
	_, err := runExec(t, srv, "", "web", "--project", "shop", "--", "true")
	if err == nil || err.Error() != msg {
		t.Fatalf("err = %v, want exactly %q", err, msg)
	}
}

func TestExec_NeedsACommandAfterTheDashes(t *testing.T) {
	srv := execAPI(t, func(http.ResponseWriter, client.ExecSessionRequest) { t.Error("must not issue a session") })
	for _, args := range [][]string{
		{"web", "--project", "shop"},
		{"web", "--project", "shop", "--"},
		{"web", "ls", "--project", "shop"},
		{"--project", "shop", "--", "ls"},
	} {
		if _, err := runExec(t, srv, "", args...); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("args %q: err = %v, want a usage error", args, err)
		}
	}
}

func TestExecCodeErrorExitsQuietly(t *testing.T) {
	var err error = &ExitCodeError{Code: 3}
	var exit *ExitCodeError
	if !errors.As(err, &exit) || exit.Code != 3 || exit.Error() != "exit status 3" {
		t.Errorf("ExitCodeError = %v", err)
	}
}

type fakeTerm struct{ restored int }

func (f *fakeTerm) MakeRaw() (func() error, error) {
	return func() error { f.restored++; return nil }, nil
}
func (f *fakeTerm) Size() (int, int, error)                 { return 90, 33, nil }
func (f *fakeTerm) Resizes(context.Context) <-chan struct{} { return make(chan struct{}) }

func runConsole(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	c := appConsoleCmd(newTestDeps(srv))
	var out bytes.Buffer
	c.SetIn(strings.NewReader(""))
	c.SetOut(&out)
	c.SetErr(&out)
	c.SilenceUsage, c.SilenceErrors = true, true
	c.SetArgs(args)
	err := c.ExecuteContext(context.Background())
	return out.String(), err
}

func TestConsole_OpensAShellOnTheTerminal(t *testing.T) {
	gw, closeGW := exectest.New()
	t.Cleanup(closeGW)
	gw.Shell = func(rw io.ReadWriter) uint32 { _, _ = rw.Write([]byte("welcome")); return 0 }
	srv := execAPI(t, issueFrom(t, gw))
	ft := &fakeTerm{}
	old := openTerminal
	openTerminal = func() (exec.Terminal, error) { return ft, nil }
	t.Cleanup(func() { openTerminal = old })
	t.Setenv("TERM", "screen-test")

	out, err := runConsole(t, srv, "web", "--project", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "welcome") {
		t.Errorf("output = %q", out)
	}
	reqs, _ := gw.Ptys()
	if len(reqs) != 1 || reqs[0].Term != "screen-test" || reqs[0].Width != 90 || reqs[0].Height != 33 {
		t.Errorf("pty-req = %+v", reqs)
	}
	if ft.restored != 1 {
		t.Errorf("terminal restored %d times, want 1", ft.restored)
	}
}

func TestConsole_RefusesWithoutATerminal(t *testing.T) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		t.Skip("stdin is a terminal")
	}
	srv := execAPI(t, func(http.ResponseWriter, client.ExecSessionRequest) { t.Error("must not issue a session") })
	_, err := runConsole(t, srv, "web", "--project", "shop")
	if err == nil || !strings.Contains(err.Error(), "heroctl exec") {
		t.Fatalf("err = %v, want a hint to use exec", err)
	}
}

func TestExecAndConsoleHelpMentionTheRequirements(t *testing.T) {
	for _, c := range []*cobra.Command{appExecCmd(&Deps{}), appConsoleCmd(&Deps{})} {
		for _, want := range []string{"enabled per deployment", "editor", "heroctl login", "personal access token"} {
			if !strings.Contains(c.Long, want) {
				t.Errorf("%s help lacks %q", c.Name(), want)
			}
		}
	}
}
