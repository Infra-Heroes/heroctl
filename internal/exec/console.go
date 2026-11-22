package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// Terminal is the local terminal a console runs on.
type Terminal interface {
	// MakeRaw puts the terminal in raw mode and returns the function that restores it.
	MakeRaw() (restore func() error, err error)
	// Size returns the current width and height in cells.
	Size() (width, height int, err error)
	// Resizes sends a value whenever the terminal size may have changed, until ctx ends.
	Resizes(ctx context.Context) <-chan struct{}
}

// Console opens a PTY session and bridges it to the local terminal. With a nil argv it
// runs the remote login shell; otherwise it runs argv (quoted as Run does) on the PTY. The
// terminal is restored on every way out. It returns the remote exit code.
func Console(ctx context.Context, c *ssh.Client, t Terminal, termName string, argv []string, stdin io.Reader, stdout io.Writer) (int, error) {
	s, err := c.NewSession()
	if err != nil {
		return 0, fmt.Errorf("open session: %w", err)
	}
	defer func() { _ = s.Close() }()

	w, h, err := t.Size()
	if err != nil {
		return 0, fmt.Errorf("terminal size: %w", err)
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := s.RequestPty(termName, h, w, modes); err != nil {
		return 0, fmt.Errorf("request pty: %w", err)
	}
	// A pty merges the program's own stderr into stdout, but the guest's errors (such as
	// "tmux: command not found") arrive on the stderr channel, which ssh would discard.
	s.Stdout, s.Stderr = stdout, stdout
	in, err := s.StdinPipe()
	if err != nil {
		return 0, fmt.Errorf("open stdin: %w", err)
	}

	restore, err := t.MakeRaw()
	if err != nil {
		return 0, fmt.Errorf("terminal raw mode: %w", err)
	}
	defer func() { _ = restore() }()

	if argv == nil {
		if err := s.Shell(); err != nil {
			return 0, fmt.Errorf("start shell (the image may have no shell, use exec instead): %w", err)
		}
	} else if err := s.Start(Quote(argv)); err != nil {
		return 0, fmt.Errorf("start %s: %w", argv[0], err)
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for range t.Resizes(rctx) {
			if w, h, err := t.Size(); err == nil {
				_ = s.WindowChange(h, w)
			}
		}
	}()
	go func() {
		_, _ = io.Copy(in, stdin)
		_ = in.Close()
	}()
	return wait(ctx, s)
}

// stdTerminal is the process's own terminal.
type stdTerminal struct{ fd int }

// OpenStdTerminal returns the terminal behind stdin, or an error when stdin is not one.
func OpenStdTerminal() (Terminal, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("console needs a terminal on stdin; use \"heroctl exec\" to run a command without one")
	}
	return &stdTerminal{fd: fd}, nil
}

func (t *stdTerminal) MakeRaw() (func() error, error) {
	old, err := term.MakeRaw(t.fd)
	if err != nil {
		return nil, err
	}
	return func() error { return term.Restore(t.fd, old) }, nil
}

func (t *stdTerminal) Size() (int, int, error) { return term.GetSize(t.fd) }
