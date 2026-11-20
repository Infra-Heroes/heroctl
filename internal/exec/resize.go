package exec

import (
	"context"
	"time"
)

// pollResizes sends on the returned channel whenever size reports a different size than
// before. It stops, and closes the channel, when ctx ends.
func pollResizes(ctx context.Context, every time.Duration, size func() (int, int, error)) <-chan struct{} {
	out := make(chan struct{}, 1)
	lastW, lastH, _ := size()
	go func() {
		defer close(out)
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				w, h, err := size()
				if err != nil || (w == lastW && h == lastH) {
					continue
				}
				lastW, lastH = w, h
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	return out
}
