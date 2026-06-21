package client

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// wsConn implements net.Conn to wrap a WebSocket connection.
type wsConn struct {
	net.Conn
	reader   *bufio.Reader
	leftover []byte
}

func (w *wsConn) Read(p []byte) (int, error) {
	if len(w.leftover) > 0 {
		n := copy(p, w.leftover)
		w.leftover = w.leftover[n:]
		return n, nil
	}

	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(w.reader, header); err != nil {
			return 0, err
		}

		fin := (header[0] & 0x80) != 0
		opcode := header[0] & 0x0f
		masked := (header[1] & 0x80) != 0
		payloadLen := int64(header[1] & 0x7f)

		switch payloadLen {
		case 126:
			lenBytes := make([]byte, 2)
			if _, err := io.ReadFull(w.reader, lenBytes); err != nil {
				return 0, err
			}
			payloadLen = int64(lenBytes[0])<<8 | int64(lenBytes[1])
		case 127:
			lenBytes := make([]byte, 8)
			if _, err := io.ReadFull(w.reader, lenBytes); err != nil {
				return 0, err
			}
			var val uint64
			for _, b := range lenBytes {
				val = (val << 8) | uint64(b)
			}
			payloadLen = int64(val)
		}

		var mask []byte
		if masked {
			mask = make([]byte, 4)
			if _, err := io.ReadFull(w.reader, mask); err != nil {
				return 0, err
			}
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(w.reader, payload); err != nil {
			return 0, err
		}

		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		if opcode == 8 { // Close frame
			return 0, io.EOF
		}

		// Skip non-data frames (ping, pong, etc.)
		if opcode != 1 && opcode != 2 {
			if !fin && payloadLen == 0 {
				continue
			}
			continue
		}

		if len(payload) == 0 {
			continue
		}

		n := copy(p, payload)
		if n < len(payload) {
			w.leftover = payload[n:]
		}
		return n, nil
	}
}

func (w *wsConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	// Generate random mask for client-to-server security
	mask := make([]byte, 4)
	if _, err := io.ReadFull(rand.Reader, mask); err != nil {
		mask = []byte{0x12, 0x34, 0x56, 0x78}
	}

	maskedPayload := make([]byte, len(p))
	for i := range p {
		maskedPayload[i] = p[i] ^ mask[i%4]
	}

	var header []byte
	byte0 := byte(0x82) // FIN set, binary frame type
	byte1 := byte(0x80) // MASK bit set

	if len(p) < 126 {
		byte1 |= byte(len(p))
		header = []byte{byte0, byte1}
	} else if len(p) <= 65535 {
		byte1 |= 126
		header = []byte{byte0, byte1, byte(len(p) >> 8), byte(len(p))}
	} else {
		byte1 |= 127
		header = make([]byte, 10)
		header[0] = byte0
		header[1] = byte1
		val := uint64(len(p))
		for i := 9; i >= 2; i-- {
			header[i] = byte(val)
			val >>= 8
		}
	}

	if _, err := w.Conn.Write(header); err != nil {
		return 0, err
	}
	if _, err := w.Conn.Write(mask); err != nil {
		return 0, err
	}
	if _, err := w.Conn.Write(maskedPayload); err != nil {
		return 0, err
	}

	return len(p), nil
}

// DialWebSocket performs a raw TCP/TLS dial and handles HTTP WebSocket upgrade handshake.
func DialWebSocket(ctx context.Context, u *url.URL, headers map[string]string) (net.Conn, error) {
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" || u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	var conn net.Conn
	var err error
	dialer := net.Dialer{}

	if u.Scheme == "wss" || u.Scheme == "https" {
		conn, err = tls.DialWithDialer(&dialer, "tcp", host, &tls.Config{
			InsecureSkipVerify: true,
		})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	pathAndQuery := u.RawPath
	if pathAndQuery == "" {
		pathAndQuery = u.Path
	}
	if pathAndQuery == "" {
		pathAndQuery = "/"
	}
	if u.RawQuery != "" {
		pathAndQuery += "?" + u.RawQuery
	}

	keyBytes := make([]byte, 16)
	_, _ = io.ReadFull(rand.Reader, keyBytes)
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)

	req := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n", pathAndQuery, u.Host, wsKey)

	for k, v := range headers {
		req += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	req += "\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("send upgrade request: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read upgrade response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		_ = conn.Close()
		return nil, fmt.Errorf("upgrade failed (status %d): %s", resp.StatusCode, string(body))
	}

	return &wsConn{
		Conn:   conn,
		reader: reader,
	}, nil
}
