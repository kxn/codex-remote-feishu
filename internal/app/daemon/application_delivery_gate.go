package daemon

import (
	"context"
	"sync"
)

// deliveryGate governs callback lifetime, not business/native ownership. Once
// closed it never reopens. A timeout leaves active deliveries tracked so stores
// cannot be closed underneath an SDK or HTTP handler that is still returning.
type applicationDeliveryGate struct {
	mu      sync.Mutex
	active  int
	closing bool
	done    chan struct{}
}

func (g *applicationDeliveryGate) begin() (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return nil, false
	}
	g.active++
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.active--
		if g.closing && g.active == 0 && g.done != nil {
			close(g.done)
			g.done = nil
		}
	}, true
}

func (g *applicationDeliveryGate) closeReady() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closing = true
	return g.active == 0
}

func (g *applicationDeliveryGate) drain(ctx context.Context) error {
	g.mu.Lock()
	g.closing = true
	if g.active == 0 {
		g.mu.Unlock()
		return nil
	}
	if g.done == nil {
		g.done = make(chan struct{})
	}
	done := g.done
	g.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
