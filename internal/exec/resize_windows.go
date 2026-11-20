//go:build windows

package exec

import (
	"context"
	"time"
)

// pollInterval is how often Windows checks the terminal size.
const pollInterval = 250 * time.Millisecond

// Resizes polls the console size: Windows has no SIGWINCH.
func (t *stdTerminal) Resizes(ctx context.Context) <-chan struct{} {
	return pollResizes(ctx, pollInterval, t.Size)
}
