package exec

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// signalNumbers maps the SSH signal names (RFC 4254 §6.10) to Linux signal numbers; the
// remote side is always a Linux guest.
var signalNumbers = map[string]int{
	"HUP": 1, "INT": 2, "QUIT": 3, "ILL": 4, "ABRT": 6, "FPE": 8, "KILL": 9,
	"USR1": 10, "SEGV": 11, "USR2": 12, "PIPE": 13, "ALRM": 14, "TERM": 15,
}

// Run runs argv on the remote side and returns its exit code: the exit status, or 128 plus
// the signal number when it was killed by a signal. stdin is sent until EOF, then closed.
func Run(ctx context.Context, c *ssh.Client, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	s, err := c.NewSession()
	if err != nil {
		return 0, fmt.Errorf("open session: %w", err)
	}
	defer func() { _ = s.Close() }()
	s.Stdout, s.Stderr = stdout, stderr
	in, err := s.StdinPipe()
	if err != nil {
		return 0, fmt.Errorf("open stdin: %w", err)
	}
	if err := s.Start(Quote(argv)); err != nil {
		return 0, fmt.Errorf("start command: %w", err)
	}
	// Copy stdin ourselves: Wait would otherwise block on a stdin that never ends.
	go func() {
		_, _ = io.Copy(in, stdin)
		_ = in.Close()
	}()
	return wait(ctx, s)
}

// wait waits for the session and turns its outcome into an exit code. Cancelling ctx
// closes the session.
func wait(ctx context.Context, s *ssh.Session) (int, error) {
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	err := s.Wait()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	var exit *ssh.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		if sig := exit.Signal(); sig != "" {
			if n, ok := signalNumbers[sig]; ok {
				return 128 + n, nil
			}
			return 255, nil
		}
		return exit.ExitStatus(), nil
	default:
		return 0, fmt.Errorf("remote session: %w", err)
	}
}
