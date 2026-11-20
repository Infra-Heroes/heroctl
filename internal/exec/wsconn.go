package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// maxMessage caps one outgoing WebSocket message; larger writes are split.
const maxMessage = 32 * 1024

// Dial opens the gateway WebSocket. The ticket travels in the Authorization header only.
func Dial(ctx context.Context, gatewayURL, ticket string) (net.Conn, error) {
	d := websocket.Dialer{HandshakeTimeout: 15 * time.Second, Proxy: http.ProxyFromEnvironment}
	ws, resp, err := d.DialContext(ctx, gatewayURL, http.Header{"Authorization": {"Bearer " + ticket}})
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("exec gateway refused the connection: HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("connect to exec gateway: %w", err)
	}
	return NewWSConn(ws), nil
}

// NewWSConn wraps a WebSocket as a net.Conn: every Write becomes binary messages and
// reads continue across message boundaries.
func NewWSConn(ws *websocket.Conn) net.Conn { return &wsConn{ws: ws} }

type wsConn struct {
	ws  *websocket.Conn
	r   io.Reader
	wmu sync.Mutex
}

func (c *wsConn) Read(p []byte) (int, error) {
	for {
		if c.r == nil {
			typ, r, err := c.ws.NextReader()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return 0, io.EOF
				}
				return 0, err
			}
			if typ != websocket.BinaryMessage {
				return 0, errors.New("exec gateway sent a non-binary message")
			}
			c.r = r
		}
		n, err := c.r.Read(p)
		if errors.Is(err, io.EOF) {
			c.r, err = nil, nil
			if n == 0 {
				continue
			}
		}
		return n, err
	}
}

func (c *wsConn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	sent := 0
	for sent < len(p) {
		end := min(sent+maxMessage, len(p))
		if err := c.ws.WriteMessage(websocket.BinaryMessage, p[sent:end]); err != nil {
			return sent, err
		}
		sent = end
	}
	return sent, nil
}

func (c *wsConn) Close() error {
	c.wmu.Lock()
	_ = c.ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	c.wmu.Unlock()
	return c.ws.Close()
}

func (c *wsConn) LocalAddr() net.Addr  { return c.ws.LocalAddr() }
func (c *wsConn) RemoteAddr() net.Addr { return c.ws.RemoteAddr() }

func (c *wsConn) SetDeadline(t time.Time) error {
	return errors.Join(c.ws.SetReadDeadline(t), c.ws.SetWriteDeadline(t))
}
func (c *wsConn) SetReadDeadline(t time.Time) error  { return c.ws.SetReadDeadline(t) }
func (c *wsConn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }
