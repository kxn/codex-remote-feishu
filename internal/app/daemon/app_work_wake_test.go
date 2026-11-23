package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommittedWorkWakeDoesNotWaitForPollOrRunLaneConcurrently(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := &teamRuntime{}
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	r.every(ctx, "test", time.Hour, func(ctx context.Context) error {
		n := active.Add(1)
		if n > maximum.Load() {
			maximum.Store(n)
		}
		defer active.Add(-1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	defer func() { cancel(); r.workers.Wait() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("lane did not start")
	}
	for range 100 {
		r.wakeLane("test")
	}
	select {
	case <-entered:
		t.Fatal("wakeup ran a second concurrent consumer")
	default:
	}
	release <- struct{}{}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("committed work waited for the hourly fallback poll")
	}
	if maximum.Load() != 1 {
		t.Fatal("lane was concurrent")
	}
	release <- struct{}{}
}
