package exec

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Infra-Heroes/heroctl/internal/client"
)

// handshakeTimeout bounds key exchange and authentication.
const handshakeTimeout = 30 * time.Second

// IssueFunc asks hero-api for an exec session for the given authorized_keys public key.
type IssueFunc func(ctx context.Context, publicKey string) (*client.ExecSession, error)

// Connect generates an ephemeral Ed25519 key, has the session issued for it, and opens an
// authenticated SSH connection through the gateway. The private key lives in memory only
// and is zeroed as soon as authentication is over (best effort).
func Connect(ctx context.Context, issue IssueFunc) (*ssh.Client, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate session key: %w", err)
	}
	wipe := func() { clear(priv); runtime.KeepAlive(priv) }
	defer wipe()

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("encode session key: %w", err)
	}
	sess, err := issue(ctx, string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(sshPub))))
	if err != nil {
		return nil, err
	}

	cfg, err := clientConfig(sess, priv)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(sess.GatewayURL)
	if err != nil {
		return nil, fmt.Errorf("parse gateway url: %w", err)
	}
	conn, err := Dial(ctx, sess.GatewayURL, sess.Ticket)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	c, chans, reqs, err := ssh.NewClientConn(conn, u.Host, cfg)
	wipe()
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

func clientConfig(sess *client.ExecSession, priv ed25519.PrivateKey) (*ssh.ClientConfig, error) {
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("session key: %w", err)
	}
	certKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(sess.Certificate))
	if err != nil {
		return nil, fmt.Errorf("parse session certificate: %w", err)
	}
	cert, ok := certKey.(*ssh.Certificate)
	if !ok {
		return nil, errors.New("session certificate is not an SSH certificate")
	}
	certSigner, err := ssh.NewCertSigner(cert, signer)
	if err != nil {
		return nil, fmt.Errorf("session certificate: %w", err)
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(sess.HostKey))
	if err != nil {
		return nil, fmt.Errorf("parse guest host key: %w", err)
	}
	return &ssh.ClientConfig{
		User:              sess.User,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(certSigner)},
		HostKeyCallback:   ssh.FixedHostKey(hostKey),
		HostKeyAlgorithms: []string{hostKey.Type()},
	}, nil
}
