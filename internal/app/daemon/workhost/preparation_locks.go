package workhost

import (
	"context"
	"sync"
)

type preparationLock struct {
	permit chan struct{}
	users  int
}

// Entries disappear when the final waiter leaves, so idle contexts do not
// accumulate process-local locks. Store CAS remains the cross-process guard.
type preparationLocks struct {
	mu      sync.Mutex
	entries map[string]*preparationLock
}

// Pool eviction must never wait while holding another context's lock.
func (p *preparationLocks) tryAcquire(key string) (func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = make(map[string]*preparationLock)
	}
	l := p.entries[key]
	if l != nil {
		return nil, false
	}
	l = &preparationLock{permit: make(chan struct{}, 1), users: 1}
	l.permit <- struct{}{}
	p.entries[key] = l
	return func() {
		<-l.permit
		p.mu.Lock()
		defer p.mu.Unlock()
		l.users--
		if l.users == 0 {
			delete(p.entries, key)
		}
	}, true
}

func (p *preparationLocks) acquire(ctx context.Context, key string) (func(), error) {
	p.mu.Lock()
	if p.entries == nil {
		p.entries = make(map[string]*preparationLock)
	}
	l := p.entries[key]
	if l == nil {
		l = &preparationLock{permit: make(chan struct{}, 1)}
		p.entries[key] = l
	}
	l.users++
	p.mu.Unlock()
	releaseRef := func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		l.users--
		if l.users == 0 {
			delete(p.entries, key)
		}
	}
	select {
	case l.permit <- struct{}{}:
		return func() { <-l.permit; releaseRef() }, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}
