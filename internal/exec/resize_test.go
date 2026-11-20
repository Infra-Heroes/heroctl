package exec

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollResizes_ReportsOnlyChanges(t *testing.T) {
	var w atomic.Int32
	w.Store(80)
	size := func() (int, int, error) { return int(w.Load()), 24, nil }
	ctx, cancel := context.WithCancel(context.Background())
	ch := pollResizes(ctx, 5*time.Millisecond, size)

	select {
	case <-ch:
		t.Fatal("reported a resize while the size did not change")
	case <-time.After(40 * time.Millisecond):
	}
	w.Store(100)
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no resize reported after the size changed")
	}
	select {
	case <-ch:
		t.Fatal("reported the same change twice")
	case <-time.After(40 * time.Millisecond):
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected value after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}
