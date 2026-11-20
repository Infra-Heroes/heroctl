//go:build !windows

package exec

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Resizes reports SIGWINCH.
func (t *stdTerminal) Resizes(ctx context.Context) <-chan struct{} {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		defer signal.Stop(sig)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sig:
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	return out
}
