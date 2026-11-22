package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Infra-Heroes/heroctl/internal/client"
	"github.com/Infra-Heroes/heroctl/internal/exec"
	"github.com/Infra-Heroes/heroctl/internal/exec/exectest"
)

type sandboxCall struct{ method, path, body string }

// sandboxAPI serves the project list and records every other request. respond may answer
// them; by default they get 200 and an empty object.
func sandboxAPI(t *testing.T, respond func(w http.ResponseWriter, r *http.Request) bool) (*httptest.Server, *[]sandboxCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []sandboxCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects" {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "p1", "name": "shop"}})
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, sandboxCall{r.Method, r.URL.Path, string(b)})
		mu.Unlock()
		if respond != nil && respond(w, r) {
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func runSandbox(t *testing.T, srv *httptest.Server, stdin string, args ...string) (string, error) {
	t.Helper()
	c := sandboxCmd(newTestDeps(srv))
	var out syncBuffer
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&out)
	c.SilenceUsage, c.SilenceErrors = true, true
	c.SetArgs(args)
	err := c.ExecuteContext(context.Background())
	return out.String(), err
}

func setTTY(t *testing.T, is bool) {
	t.Helper()
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return is }
	t.Cleanup(func() { stdinIsTerminal = old })
}

func TestSandboxCreate_SendsKindAndOnlyTheFlagsThatWereSet(t *testing.T) {
	srv, calls := sandboxAPI(t, nil)
	if _, err := runSandbox(t, srv, "", "create", "dev", "--project", "shop"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].method != "POST" || (*calls)[0].path != "/api/v1/projects/p1/deployments" {
		t.Fatalf("calls = %+v", *calls)
	}
	if got := (*calls)[0].body; got != `{"kind":"sandbox","app_name":"dev"}` {
		t.Errorf("body = %s", got)
	}
}

func TestSandboxCreate_SizesAndSecrets(t *testing.T) {
	srv, calls := sandboxAPI(t, nil)
	_, err := runSandbox(t, srv, "", "create", "dev", "--project", "shop",
		"--cpu", "4", "--memory", "8192", "--disk", "50", "--secret", "GH_TOKEN", "--secret", "NPM_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte((*calls)[0].body), &body); err != nil {
		t.Fatal(err)
	}
	env, _ := body["env"].(map[string]any)
	if body["kind"] != "sandbox" || body["cpu"] != float64(4) || body["memory_mb"] != float64(8192) || body["disk_gb"] != float64(50) ||
		len(env) != 2 || env["GH_TOKEN"] != "secret:GH_TOKEN" || env["NPM_TOKEN"] != "secret:NPM_TOKEN" {
		t.Errorf("body = %v", body)
	}
	for _, k := range []string{"image", "port", "volumes", "min_replicas"} {
		if _, ok := body[k]; ok {
			t.Errorf("body has %q, the server owns it", k)
		}
	}
}

func TestSandboxCreate_RejectsBadInputBeforeTheAPI(t *testing.T) {
	srv, calls := sandboxAPI(t, nil)
	for _, args := range [][]string{
		{"create", "Bad_Name", "--project", "shop"},
		{"create", "dev", "--project", "shop", "--secret", "not-a-key"},
	} {
		if _, err := runSandbox(t, srv, "", args...); err == nil {
			t.Errorf("args %q: no error", args)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %+v", *calls)
	}
}

func TestSandboxList_ShowsOnlySandboxes(t *testing.T) {
	srv, _ := sandboxAPI(t, func(w http.ResponseWriter, r *http.Request) bool {
		_ = json.NewEncoder(w).Encode([]client.Deployment{
			{AppName: "web", Kind: "app", Status: "running"},
			{AppName: "dev", Kind: "sandbox", Status: "running", CPU: 2, MemoryMB: 2048},
			{AppName: "old", Status: "running"}, // no kind
			{AppName: "scratch", Kind: "sandbox", Status: "stopped"},
		})
		return true
	})
	out, err := runSandbox(t, srv, "", "list", "--project", "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dev", "scratch", "stopped"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"web", "old"} {
		if strings.Contains(out, not) {
			t.Errorf("output has %q:\n%s", not, out)
		}
	}
}

func TestSandboxList_Empty(t *testing.T) {
	srv, _ := sandboxAPI(t, func(w http.ResponseWriter, r *http.Request) bool {
		_, _ = io.WriteString(w, `[{"app_name":"web","kind":"app"}]`)
		return true
	})
	out, err := runSandbox(t, srv, "", "list", "--project", "shop")
	if err != nil || !strings.Contains(out, "No sandboxes") {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestSandboxStartAndStop(t *testing.T) {
	srv, calls := sandboxAPI(t, nil)
	for _, verb := range []string{"start", "stop"} {
		if _, err := runSandbox(t, srv, "", verb, "dev", "--project", "shop"); err != nil {
			t.Fatal(err)
		}
	}
	want := []sandboxCall{
		{"POST", "/api/v1/projects/p1/deployments/dev/start", ""},
		{"POST", "/api/v1/projects/p1/deployments/dev/stop", ""},
	}
	if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
		t.Errorf("calls = %+v", *calls)
	}
}

func TestSandboxDeleteAndReset_Confirmation(t *testing.T) {
	for _, tc := range []struct {
		verb, method, path string
	}{
		{"delete", "DELETE", "/api/v1/projects/p1/deployments/dev"},
		{"reset", "POST", "/api/v1/projects/p1/deployments/dev/reset"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			t.Run("yes skips the question, even without a terminal", func(t *testing.T) {
				setTTY(t, false)
				srv, calls := sandboxAPI(t, nil)
				if _, err := runSandbox(t, srv, "", tc.verb, "dev", "--project", "shop", "--yes"); err != nil {
					t.Fatal(err)
				}
				if len(*calls) != 1 || (*calls)[0].method != tc.method || (*calls)[0].path != tc.path {
					t.Errorf("calls = %+v", *calls)
				}
			})
			t.Run("refuses without a terminal", func(t *testing.T) {
				setTTY(t, false)
				srv, calls := sandboxAPI(t, nil)
				_, err := runSandbox(t, srv, "y\n", tc.verb, "dev", "--project", "shop")
				if err == nil || !strings.Contains(err.Error(), "--yes") {
					t.Errorf("err = %v, want a hint to use --yes", err)
				}
				if len(*calls) != 0 {
					t.Errorf("calls = %+v", *calls)
				}
			})
			t.Run("a terminal is asked and yes goes ahead", func(t *testing.T) {
				setTTY(t, true)
				srv, calls := sandboxAPI(t, nil)
				if _, err := runSandbox(t, srv, "y\n", tc.verb, "dev", "--project", "shop"); err != nil {
					t.Fatal(err)
				}
				if len(*calls) != 1 || (*calls)[0].path != tc.path {
					t.Errorf("calls = %+v", *calls)
				}
			})
			t.Run("a terminal is asked and anything else aborts", func(t *testing.T) {
				setTTY(t, true)
				srv, calls := sandboxAPI(t, nil)
				out, err := runSandbox(t, srv, "n\n", tc.verb, "dev", "--project", "shop")
				if err != nil || !strings.Contains(out, "Aborted") {
					t.Errorf("out = %q, err = %v", out, err)
				}
				if len(*calls) != 0 {
					t.Errorf("calls = %+v", *calls)
				}
			})
		})
	}
}

func TestSandboxReset_ShowsTheConflictMessage(t *testing.T) {
	const msg = "sandbox dev is running; stop it before resetting"
	srv, _ := sandboxAPI(t, func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return true
	})
	_, err := runSandbox(t, srv, "", "reset", "dev", "--project", "shop", "--yes")
	if err == nil || !strings.Contains(err.Error(), msg) {
		t.Errorf("err = %v, want %q", err, msg)
	}
}

func TestSandboxHelp_HasThePromise(t *testing.T) {
	c := sandboxCmd(&Deps{})
	for _, want := range []string{
		"Everything on your sandbox's filesystem survives stop, start, restarts and host moves until you `reset` or `delete` the sandbox. Running processes do not survive a stop or a host failure. A hardware failure can lose only the last seconds of unflushed writes. If your balance stays exhausted for 30 days, the sandbox and its data are deleted; the debt remains.",
		"single replica",
	} {
		if !strings.Contains(c.Long, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

// sandboxExecAPI serves the sandbox "dev" in project "shop" like execAPI serves "web".
func sandboxExecAPI(t *testing.T, gw *exectest.Server) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "p1", "name": "shop"}})
		case "POST /api/v1/projects/p1/deployments/dev/exec-sessions":
			var body client.ExecSessionRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			issueFrom(t, gw)(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSandboxConsole_RunsTmuxUnlessRaw(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string // the argv the server got, "" for a plain shell request
	}{
		{"tmux by default", nil, "tmux|new|-A|-s|main"},
		{"raw is a plain shell", []string{"--raw"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw, closeGW := exectest.New()
			t.Cleanup(closeGW)
			var mu sync.Mutex
			var gotArgv, shell string
			gw.Run = func(argv []string, _ io.Reader, stdout, _ io.Writer) (uint32, string) {
				mu.Lock()
				gotArgv = strings.Join(argv, "|")
				mu.Unlock()
				_, _ = stdout.Write([]byte("in tmux"))
				return 0, ""
			}
			gw.Shell = func(rw io.ReadWriter) uint32 {
				mu.Lock()
				shell = "shell"
				mu.Unlock()
				_, _ = rw.Write([]byte("in shell"))
				return 0
			}
			srv := sandboxExecAPI(t, gw)
			ft := &fakeTerm{}
			old := openTerminal
			openTerminal = func() (exec.Terminal, error) { return ft, nil }
			t.Cleanup(func() { openTerminal = old })

			out, err := runSandbox(t, srv, "", append([]string{"console", "dev", "--project", "shop"}, tc.args...)...)
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if tc.want != "" {
				if gotArgv != tc.want || shell != "" || !strings.Contains(out, "in tmux") {
					t.Errorf("argv = %q, shell = %q, out = %q", gotArgv, shell, out)
				}
			} else if gotArgv != "" || shell != "shell" || !strings.Contains(out, "in shell") {
				t.Errorf("argv = %q, shell = %q, out = %q", gotArgv, shell, out)
			}
			if reqs, _ := gw.Ptys(); len(reqs) != 1 {
				t.Errorf("pty-reqs = %+v, want one", reqs)
			}
			if ft.restored != 1 {
				t.Errorf("terminal restored %d times", ft.restored)
			}
		})
	}
}

func TestSandboxExec_RunsTheCommandAndPassesTheExitCode(t *testing.T) {
	gw, closeGW := exectest.New()
	t.Cleanup(closeGW)
	gw.Run = func(argv []string, _ io.Reader, stdout, _ io.Writer) (uint32, string) {
		_, _ = stdout.Write([]byte("[" + strings.Join(argv, "|") + "]"))
		return 7, ""
	}
	srv := sandboxExecAPI(t, gw)
	out, err := runSandbox(t, srv, "", "exec", "dev", "--project", "shop", "--", "echo", "it's", "a b")
	var exit *ExitCodeError
	if !errors.As(err, &exit) || exit.Code != 7 {
		t.Fatalf("err = %v, want exit code 7", err)
	}
	if !strings.Contains(out, "[echo|it's|a b]") {
		t.Errorf("out = %q", out)
	}
}

func TestSandboxExec_NeedsACommandAfterTheDashes(t *testing.T) {
	srv, calls := sandboxAPI(t, nil)
	for _, args := range [][]string{
		{"exec", "dev", "--project", "shop"},
		{"exec", "dev", "--project", "shop", "--"},
		{"exec", "dev", "ls", "--project", "shop"},
	} {
		if _, err := runSandbox(t, srv, "", args...); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("args %q: err = %v, want a usage error", args, err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %+v", *calls)
	}
}
