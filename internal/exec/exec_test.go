package exec_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Infra-Heroes/heroctl/internal/client"
	"github.com/Infra-Heroes/heroctl/internal/exec"
	"github.com/Infra-Heroes/heroctl/internal/exec/exectest"
)

func connect(t *testing.T, srv *exectest.Server, mutate func(*client.ExecSession)) (*ssh.Client, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	c, err := exec.Connect(ctx, func(_ context.Context, pub string) (*client.ExecSession, error) {
		sess, err := srv.Session(pub)
		if err == nil && mutate != nil {
			mutate(sess)
		}
		return sess, err
	})
	if c != nil {
		t.Cleanup(func() { _ = c.Close() })
	}
	return c, err
}

func newServer(t *testing.T) *exectest.Server {
	t.Helper()
	srv, closeFn := exectest.New()
	t.Cleanup(closeFn)
	return srv
}

func TestQuote(t *testing.T) {
	if got, want := exec.Quote([]string{"echo", "it's"}), `'echo' 'it'\''s'`; got != want {
		t.Errorf("Quote = %s, want %s", got, want)
	}
	for _, argv := range [][]string{
		{"ls"},
		{"echo", ""},
		{"", ""},
		{"a b", "  ", "c\td"},
		{"'"},
		{"''"},
		{"a'b'c", `\'`, `\`, `'\'`},
		{"$(rm -rf /)", "`id`", "a;b", "a|b", "*", "~", "!", "$HOME", `"x"`},
		{"line1\nline2", "\x00nul", "ünï cødé", "tab\t"},
		{" lead", "trail "},
	} {
		got, err := exectest.SplitCommand(exec.Quote(argv))
		if err != nil || !reflect.DeepEqual(got, argv) {
			t.Errorf("round trip of %q = %q, %v (quoted: %s)", argv, got, err, exec.Quote(argv))
		}
	}
}

func TestRun_ArgvBinaryIOAndExitCodes(t *testing.T) {
	srv := newServer(t)
	var mu sync.Mutex
	var gotArgv []string
	srv.Run = func(argv []string, stdin io.Reader, stdout, stderr io.Writer) (uint32, string) {
		mu.Lock()
		gotArgv = argv
		mu.Unlock()
		switch argv[0] {
		case "cat":
			_, _ = io.Copy(stdout, stdin)
			_, _ = stderr.Write([]byte{0xff, 0x00, 0xfe})
			return 7, ""
		case "term":
			return 0, "TERM"
		case "kill":
			return 0, "KILL"
		}
		return 0, ""
	}
	c, err := connect(t, srv, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	payload := make([]byte, 1<<20+13)
	_, _ = rand.Read(payload)
	for i := range 256 {
		payload[i] = byte(i)
	}
	var out, errOut bytes.Buffer
	argv := []string{"cat", "it's", "", "a b"}
	code, err := exec.Run(ctx, c, argv, bytes.NewReader(payload), &out, &errOut)
	if err != nil || code != 7 {
		t.Fatalf("code, err = %d, %v", code, err)
	}
	if !bytes.Equal(out.Bytes(), payload) {
		t.Errorf("stdout differs: got %d bytes, want %d", out.Len(), len(payload))
	}
	if !bytes.Equal(errOut.Bytes(), []byte{0xff, 0x00, 0xfe}) {
		t.Errorf("stderr = %x", errOut.Bytes())
	}
	mu.Lock()
	if !reflect.DeepEqual(gotArgv, argv) {
		t.Errorf("server saw argv %q, want %q", gotArgv, argv)
	}
	mu.Unlock()

	for _, tc := range []struct {
		cmd  string
		want int
	}{{"true", 0}, {"term", 143}, {"kill", 137}} {
		if code, err := exec.Run(ctx, c, []string{tc.cmd}, strings.NewReader(""), io.Discard, io.Discard); err != nil || code != tc.want {
			t.Errorf("%s: code, err = %d, %v, want %d", tc.cmd, code, err, tc.want)
		}
	}
}

func TestConnect_HostKeyMismatchAbortsBeforeAuth(t *testing.T) {
	srv := newServer(t)
	_, err := connect(t, srv, func(s *client.ExecSession) { s.HostKey = exectest.WrongHostKey() })
	if err == nil {
		t.Fatal("connected despite a wrong host key")
	}
	if n := srv.AuthAttempts(); n != 0 {
		t.Errorf("server saw %d authentication attempts, want 0", n)
	}
}

func TestConnect_AuthenticatesWithTheCertificate(t *testing.T) {
	srv := newServer(t)
	if _, err := connect(t, srv, nil); err != nil {
		t.Fatal(err)
	}
	if n := srv.AuthAttempts(); n != 1 {
		t.Errorf("authentication attempts = %d, want 1", n)
	}
}

func TestConnect_GatewayRefusesTicket(t *testing.T) {
	srv := newServer(t)
	_, err := connect(t, srv, func(s *client.ExecSession) { s.Ticket = "stale" })
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want the gateway's refusal", err)
	}
}

// fakeTerm records what Console does to the terminal.
type fakeTerm struct {
	mu       sync.Mutex
	w, h     int
	rawCalls int
	restored int
	resize   chan struct{}
}

func newFakeTerm() *fakeTerm { return &fakeTerm{w: 100, h: 30, resize: make(chan struct{}, 4)} }

func (f *fakeTerm) MakeRaw() (func() error, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rawCalls++
	return func() error { f.mu.Lock(); f.restored++; f.mu.Unlock(); return nil }, nil
}

func (f *fakeTerm) Size() (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.w, f.h, nil
}

func (f *fakeTerm) Resizes(context.Context) <-chan struct{} { return f.resize }

func (f *fakeTerm) counts() (raw, restored int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rawCalls, f.restored
}

func TestConsole_PtyResizeAndRestore(t *testing.T) {
	srv := newServer(t)
	release, started := make(chan struct{}), make(chan struct{})
	srv.Shell = func(rw io.ReadWriter) uint32 {
		close(started)
		_, _ = rw.Write([]byte("hello\r\n"))
		<-release
		return 3
	}
	c, err := connect(t, srv, nil)
	if err != nil {
		t.Fatal(err)
	}
	ft := newFakeTerm()
	var out bytes.Buffer
	var outMu sync.Mutex
	done := make(chan struct{})
	var code int
	var runErr error
	go func() {
		defer close(done)
		code, runErr = exec.Console(context.Background(), c, ft, "xterm-test", strings.NewReader(""), lockedWriter{&out, &outMu})
	}()

	<-started
	ft.mu.Lock()
	ft.w, ft.h = 120, 40
	ft.mu.Unlock()
	ft.resize <- struct{}{}
	if err := srv.WaitChange(); err != nil {
		t.Fatal(err)
	}
	if raw, restored := ft.counts(); raw != 1 || restored != 0 {
		t.Errorf("while running: raw=%d restored=%d, want 1 and 0", raw, restored)
	}
	close(release)
	<-done
	if runErr != nil || code != 3 {
		t.Fatalf("code, err = %d, %v", code, runErr)
	}
	reqs, changes := srv.Ptys()
	if len(reqs) != 1 || reqs[0].Term != "xterm-test" || reqs[0].Width != 100 || reqs[0].Height != 30 {
		t.Errorf("pty-req = %+v", reqs)
	}
	if len(changes) != 1 || changes[0].Width != 120 || changes[0].Height != 40 {
		t.Errorf("window-change = %+v", changes)
	}
	outMu.Lock()
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("output = %q", out.String())
	}
	outMu.Unlock()
	if raw, restored := ft.counts(); raw != 1 || restored != 1 {
		t.Errorf("after exit: raw=%d restored=%d, want 1 and 1", raw, restored)
	}
}

func TestConsole_RestoresWhenShellFails(t *testing.T) {
	srv := newServer(t) // no Shell handler: the server refuses the shell request
	c, err := connect(t, srv, nil)
	if err != nil {
		t.Fatal(err)
	}
	ft := newFakeTerm()
	_, err = exec.Console(context.Background(), c, ft, "xterm", strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no shell") {
		t.Errorf("err = %v, want a hint about images without a shell", err)
	}
	if raw, restored := ft.counts(); raw != 1 || restored != 1 {
		t.Errorf("raw=%d restored=%d, want 1 and 1", raw, restored)
	}
}

func TestConsole_RestoresOnCancel(t *testing.T) {
	srv := newServer(t)
	started := make(chan struct{})
	srv.Shell = func(rw io.ReadWriter) uint32 {
		close(started)
		_, _ = io.Copy(io.Discard, rw) // runs until the connection goes away
		return 0
	}
	c, err := connect(t, srv, nil)
	if err != nil {
		t.Fatal(err)
	}
	ft := newFakeTerm()
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	done := make(chan error, 1)
	go func() {
		_, err := exec.Console(ctx, c, ft, "xterm", pr, io.Discard)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Console returned no error after cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Console did not return after cancel")
	}
	if raw, restored := ft.counts(); raw != 1 || restored != 1 {
		t.Errorf("raw=%d restored=%d, want 1 and 1", raw, restored)
	}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
