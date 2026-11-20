// Package exectest is an in-process stand-in for the exec path in tests: a WebSocket
// gateway endpoint in front of an SSH server that behaves like hero-guest (certificate
// auth, exec grammar of contract §4, pty, shell).
package exectest

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/Infra-Heroes/heroctl/internal/client"
	"github.com/Infra-Heroes/heroctl/internal/exec"
)

// Ticket is the only bearer value the gateway endpoint accepts.
const Ticket = "test-ticket"

// Pty is a pty-req the server received.
type Pty struct {
	Term          string
	Width, Height uint32
}

// Server is the gateway endpoint plus the SSH server behind it.
type Server struct {
	// Run handles an exec request: argv is the parsed command. It returns the exit status,
	// or a signal name (without SIG) when the process was "killed".
	Run func(argv []string, stdin io.Reader, stdout, stderr io.Writer) (status uint32, signal string)
	// Shell handles a shell request on a pty; rw is the pty. It returns the exit status.
	Shell func(rw io.ReadWriter) uint32

	URL string

	hostKey ssh.Signer
	ca      ssh.Signer
	ws      *httptest.Server

	mu       sync.Mutex
	authTry  int
	ptys     []Pty
	winch    []Pty
	chanEvts chan struct{}
}

// New starts the server; it is closed with t.Cleanup via the returned closer.
func New() (*Server, func()) {
	s := &Server{chanEvts: make(chan struct{}, 64)}
	s.hostKey = newSigner()
	s.ca = newSigner()
	up := websocket.Upgrader{}
	s.ws = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+Ticket {
			http.Error(w, "bad ticket", http.StatusUnauthorized)
			return
		}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		s.serve(exec.NewWSConn(c))
	}))
	s.URL = "ws" + strings.TrimPrefix(s.ws.URL, "http") + "/v1/connect"
	return s, s.ws.Close
}

func newSigner() ssh.Signer {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	sg, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		panic(err)
	}
	return sg
}

// HostKey returns the server's host key in authorized_keys form.
func (s *Server) HostKey() string { return string(ssh.MarshalAuthorizedKey(s.hostKey.PublicKey())) }

// WrongHostKey returns some other host key in authorized_keys form.
func WrongHostKey() string { return string(ssh.MarshalAuthorizedKey(newSigner().PublicKey())) }

// AuthAttempts is the number of public-key authentication attempts the server saw.
func (s *Server) AuthAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authTry
}

// Ptys returns the pty-req and window-change requests received so far.
func (s *Server) Ptys() (reqs, changes []Pty) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Pty(nil), s.ptys...), append([]Pty(nil), s.winch...)
}

// WaitChange blocks until a window-change arrived, or fails after a timeout.
func (s *Server) WaitChange() error {
	select {
	case <-s.chanEvts:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("no window-change received")
	}
}

// Session signs a certificate for publicKey like hero-api would and returns the answer of
// the issuance endpoint.
func (s *Server) Session(publicKey string) (*client.ExecSession, error) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return nil, err
	}
	cert := &ssh.Certificate{
		Key: pub, Serial: 1, CertType: ssh.UserCert, KeyId: "sess:test",
		ValidPrincipals: []string{"hero"},
		ValidAfter:      0,
		ValidBefore:     ssh.CertTimeInfinity,
		Permissions: ssh.Permissions{
			CriticalOptions: map[string]string{"instance@infra-heroes.de": "inst-1"},
			Extensions:      map[string]string{"permit-pty": ""},
		},
	}
	if err := cert.SignCert(rand.Reader, s.ca); err != nil {
		return nil, err
	}
	return &client.ExecSession{
		GatewayURL:  s.URL,
		Ticket:      Ticket,
		Certificate: string(ssh.MarshalAuthorizedKey(cert)),
		HostKey:     s.HostKey(),
		User:        "hero",
	}, nil
}

func (s *Server) serve(conn net.Conn) {
	checker := &ssh.CertChecker{
		IsUserAuthority:          func(k ssh.PublicKey) bool { return string(k.Marshal()) == string(s.ca.PublicKey().Marshal()) },
		SupportedCriticalOptions: []string{"instance@infra-heroes.de"},
	}
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() != "hero" {
				return nil, errors.New("wrong user")
			}
			return checker.Authenticate(c, k)
		},
	}
	cfg.AuthLogCallback = func(_ ssh.ConnMetadata, method string, _ error) {
		if method == "publickey" {
			s.mu.Lock()
			s.authTry++
			s.mu.Unlock()
		}
	}
	cfg.AddHostKey(s.hostKey)
	sc, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer func() { _ = sc.Close() }()
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			return
		}
		go s.session(ch, creqs)
	}
}

func (s *Server) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer func() { _ = ch.Close() }()
	var hasPty bool
	for r := range reqs {
		switch r.Type {
		case "pty-req":
			var p struct {
				Term         string
				W, H, PW, PH uint32
				Modes        string
			}
			if err := ssh.Unmarshal(r.Payload, &p); err != nil {
				_ = r.Reply(false, nil)
				continue
			}
			hasPty = true
			s.mu.Lock()
			s.ptys = append(s.ptys, Pty{Term: p.Term, Width: p.W, Height: p.H})
			s.mu.Unlock()
			_ = r.Reply(true, nil)
		case "window-change":
			var p struct{ W, H, PW, PH uint32 }
			if err := ssh.Unmarshal(r.Payload, &p); err == nil {
				s.mu.Lock()
				s.winch = append(s.winch, Pty{Width: p.W, Height: p.H})
				s.mu.Unlock()
				select {
				case s.chanEvts <- struct{}{}:
				default:
				}
			}
		case "exec":
			var p struct{ Cmd string }
			if err := ssh.Unmarshal(r.Payload, &p); err != nil {
				_ = r.Reply(false, nil)
				continue
			}
			words, err := SplitCommand(p.Cmd)
			if err != nil || s.Run == nil {
				_ = r.Reply(false, nil)
				continue
			}
			_ = r.Reply(true, nil)
			go func() {
				status, sig := s.Run(words, ch, ch, ch.Stderr())
				finish(ch, status, sig)
				_ = ch.Close()
			}()
		case "shell":
			if !hasPty || s.Shell == nil {
				_ = r.Reply(false, nil)
				continue
			}
			_ = r.Reply(true, nil)
			go func() {
				finish(ch, s.Shell(ch), "")
				_ = ch.Close()
			}()
		default:
			_ = r.Reply(false, nil)
		}
	}
}

func finish(ch ssh.Channel, status uint32, sig string) {
	if sig != "" {
		_, _ = ch.SendRequest("exit-signal", false, ssh.Marshal(struct {
			Signal     string
			CoreDumped bool
			Msg, Lang  string
		}{Signal: sig}))
		return
	}
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
}

// SplitCommand parses the exec grammar of contract §4: words separated by single spaces,
// each word one or more single-quoted segments, with \' as the only escape between them.
// Anything else is an error.
func SplitCommand(s string) ([]string, error) {
	var words []string
	i := 0
	for {
		var w strings.Builder
		start := i
		for i < len(s) && s[i] != ' ' {
			switch {
			case s[i] == '\'':
				end := strings.IndexByte(s[i+1:], '\'')
				if end < 0 {
					return nil, errors.New("unterminated quote")
				}
				w.WriteString(s[i+1 : i+1+end])
				i += end + 2
			case strings.HasPrefix(s[i:], `\'`):
				w.WriteByte('\'')
				i += 2
			default:
				return nil, fmt.Errorf("unquoted byte %q at %d", s[i], i)
			}
		}
		if i == start {
			return nil, errors.New("empty word position")
		}
		words = append(words, w.String())
		if i == len(s) {
			return words, nil
		}
		i++ // the single separating space
		if i == len(s) {
			return nil, errors.New("trailing space")
		}
	}
}
