package client

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestWebSocketConnRead(t *testing.T) {
	// A helper to construct a raw framed WebSocket payload from server to client
	// Server to client frames are NOT masked.
	buildServerFrame := func(opcode byte, payload []byte) []byte {
		var frame []byte
		byte0 := byte(0x80) | (opcode & 0x0f) // FIN set
		frame = append(frame, byte0)

		length := len(payload)
		if length < 126 {
			frame = append(frame, byte(length))
		} else if length <= 65535 {
			frame = append(frame, 126, byte(length>>8), byte(length))
		} else {
			frame = append(frame, 127)
			for i := 7; i >= 0; i-- {
				frame = append(frame, byte(length>>(i*8)))
			}
		}

		frame = append(frame, payload...)
		return frame
	}

	t.Run("basic reading of text and binary frame", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		defer func() { _ = serverConn.Close() }()
		defer func() { _ = clientConn.Close() }()

		ws := &wsConn{
			Conn:   clientConn,
			reader: bufio.NewReader(clientConn),
		}

		go func() {
			_, _ = serverConn.Write(buildServerFrame(1, []byte("hello"))) // Text frame
			_, _ = serverConn.Write(buildServerFrame(2, []byte("world"))) // Binary frame
		}()

		buf := make([]byte, 1024)
		n, err := ws.Read(buf)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if string(buf[:n]) != "hello" {
			t.Errorf("expected 'hello', got %q", string(buf[:n]))
		}

		n, err = ws.Read(buf)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if string(buf[:n]) != "world" {
			t.Errorf("expected 'world', got %q", string(buf[:n]))
		}
	})

	t.Run("close frame handles EOF", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		defer func() { _ = serverConn.Close() }()
		defer func() { _ = clientConn.Close() }()

		ws := &wsConn{
			Conn:   clientConn,
			reader: bufio.NewReader(clientConn),
		}

		go func() {
			_, _ = serverConn.Write(buildServerFrame(8, []byte("bye"))) // Close frame
		}()

		buf := make([]byte, 1024)
		_, err := ws.Read(buf)
		if err != io.EOF {
			t.Errorf("expected io.EOF, got: %v", err)
		}
	})
}

func TestWebSocketConnWrite(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	ws := &wsConn{
		Conn:   clientConn,
		reader: bufio.NewReader(clientConn),
	}

	go func() {
		_, _ = ws.Write([]byte("hello client"))
	}()

	reader := bufio.NewReader(serverConn)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		t.Fatalf("failed to read header: %v", err)
	}

	opcode := header[0] & 0x0f
	if opcode != 2 {
		t.Errorf("expected binary opcode 2, got %d", opcode)
	}

	masked := (header[1] & 0x80) != 0
	if !masked {
		t.Error("expected client frame to be masked")
	}

	payloadLen := int(header[1] & 0x7f)
	if payloadLen != len("hello client") {
		t.Errorf("expected len %d, got %d", len("hello client"), payloadLen)
	}

	mask := make([]byte, 4)
	if _, err := io.ReadFull(reader, mask); err != nil {
		t.Fatalf("failed to read mask: %v", err)
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatalf("failed to read payload: %v", err)
	}

	for i := range payload {
		payload[i] ^= mask[i%4]
	}

	if string(payload) != "hello client" {
		t.Errorf("expected 'hello client', got %q", string(payload))
	}
}

func TestDialWebSocket(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Connection", "Upgrade")
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	u.Scheme = "ws"

	ctx := context.Background()
	conn, err := DialWebSocket(ctx, u, map[string]string{"X-Test": "yes"})
	if err != nil {
		t.Fatalf("unexpected dial error: %v", err)
	}
	_ = conn.Close()
}
