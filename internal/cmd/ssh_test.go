package cmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Infra-Heroes/heroctl/internal/auth"
	"github.com/Infra-Heroes/heroctl/internal/client"
)

func buildServerFrame(opcode byte, payload []byte) []byte {
	var frame []byte
	byte0 := byte(0x80) | (opcode & 0x0f) // FIN set
	frame = append(frame, byte0)

	length := len(payload)
	if length < 126 {
		frame = append(frame, byte(length))
	} else {
		frame = append(frame, 126, byte(length>>8), byte(length))
	}

	frame = append(frame, payload...)
	return frame
}

func readClientFrame(r io.Reader) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", err
	}
	masked := (header[1] & 0x80) != 0
	payloadLen := int(header[1] & 0x7f)
	if payloadLen == 126 {
		lenBytes := make([]byte, 2)
		if _, err := io.ReadFull(r, lenBytes); err != nil {
			return "", err
		}
		payloadLen = int(lenBytes[0])<<8 | int(lenBytes[1])
	}
	var mask []byte
	if masked {
		mask = make([]byte, 4)
		if _, err := io.ReadFull(r, mask); err != nil {
			return "", err
		}
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return "", err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return string(payload), nil
}

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


