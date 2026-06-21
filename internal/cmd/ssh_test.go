package cmd

import (
	"bytes"
	"context"

	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/auth"
	"github.com/Infra-Heroes/heroctl/internal/client"
)



func TestSSHCommand_Direct(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/projects" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"p-123","name":"my-project"}]`))
			return
		}

		if strings.Contains(r.URL.Path, "/deployments/my-app/ssh") {
			hj, ok := w.(http.Hijacker)
			if !ok {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			defer func() { _ = conn.Close() }()

			_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: tcp\r\nConnection: Upgrade\r\n\r\n"))

			// Read plain text from stdin
			buf := make([]byte, 1024)
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if string(buf[:n]) == "hello stdin" {
				_, _ = conn.Write([]byte("hello stdout"))
			}
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	tok := &auth.Token{
		AccessToken: "mock-token",
		Expiry:      time.Now().Add(1 * time.Hour),
	}
	c := client.New(ts.URL, "auth.test.com", "client-id", tok)
	deps := &Deps{
		Token:  tok,
		Client: c,
	}

	cmd := sshCmd(deps)
	cmd.SetArgs([]string{"my-app", "--project", "my-project"})

	var stdin bytes.Buffer
	stdin.WriteString("hello stdin")
	cmd.SetIn(&stdin)

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := cmd.ExecuteContext(ctx)
	if err != nil {
		t.Fatalf("command execution failed: %v", err)
	}

	if !strings.Contains(stdout.String(), "hello stdout") {
		t.Errorf("expected 'hello stdout' in output, got %q", stdout.String())
	}
}
