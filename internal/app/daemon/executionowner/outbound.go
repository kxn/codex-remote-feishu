package executionowner

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrDraining = errors.New("native carrier outbound is draining")

// BeforeSendError proves only that this call did not invoke the transport. The
// application must still settle any command it durably prepared before failure.
type BeforeSendError struct{ Err error }

func (e *BeforeSendError) Error() string             { return e.Err.Error() }
func (e *BeforeSendError) Unwrap() error             { return e.Err }
func (e *BeforeSendError) NativeCommandUnsent() bool { return true }

// Outbound is the single daemon's send/retirement boundary. It is NOT a durable
// migration fence: before Drain, the application must persist its drain intent;
// on startup it must restore those drains before exposing any ingress/senders.
// All callers sharing a relay must share this instance. The daemon process lock
// excludes a second process; the registry alone cannot drain another sender.
type Outbound struct {
	registry *Registry
	mu       sync.Mutex
	carriers map[string]*outboundCarrier
}

type outboundCarrier struct {
	registration Registration
	active       int
	draining     bool
	drained      chan struct{}
	idle         chan struct{}
}

func NewOutbound(registry *Registry) (*Outbound, error) {
	if registry == nil {
		return nil, fmt.Errorf("execution registry required")
	}
	return &Outbound{registry: registry, carriers: make(map[string]*outboundCarrier)}, nil
}

func (b *Outbound) carrierLocked(reg Registration) (*outboundCarrier, error) {
	if err := reg.validate(); err != nil {
		return nil, err
	}
	c := b.carriers[reg.Carrier.Incarnation]
	if c != nil {
		if c.registration != reg {
			return nil, ErrConflict
		}
		return c, nil
	}
	c = &outboundCarrier{registration: reg, drained: make(chan struct{}), idle: make(chan struct{})}
	close(c.idle)
	b.carriers[reg.Carrier.Incarnation] = c
	return c, nil
}

// Reject an invented first registration before it can occupy an incarnation's
// gate. Existing entries also support exact drain/transfer replay after retire.
func (b *Outbound) checkRegistration(ctx context.Context, reg Registration) error {
	if err := reg.validate(); err != nil {
		return err
	}
	b.mu.Lock()
	c := b.carriers[reg.Carrier.Incarnation]
	b.mu.Unlock()
	if c != nil {
		if c.registration != reg {
			return ErrConflict
		}
		return nil
	}
	owner, err := b.registry.Resolve(ctx, reg.Carrier, reg.Owner.Epoch)
	if err != nil {
		return err
	}
	if owner != reg.Owner {
		return ErrConflict
	}
	return nil
}

// Send holds no database transaction during network I/O. prepare commits the
// application's canonical command under the exact ownership lease. Drain waits
// for both preparation and the transport call, closing the lease/send gap.
// A transport error is returned unchanged: it is not proof of non-delivery.
func (b *Outbound) Send(ctx context.Context, reg Registration, prepare, send func() error) error {
	if prepare == nil {
		return &BeforeSendError{fmt.Errorf("prepare callback required")}
	}
	return b.SendLeased(ctx, reg, func(_ *Lease) error { return prepare() }, send)
}

// SendLeased supplies a scoped capability to journals that must validate the
// same owner while committing preparation. They call Lease.Run, not WithLease.
func (b *Outbound) SendLeased(ctx context.Context, reg Registration, prepare func(*Lease) error, send func() error) error {
	if prepare == nil || send == nil {
		return &BeforeSendError{fmt.Errorf("prepare and transport callbacks required")}
	}
	if err := ctx.Err(); err != nil {
		return &BeforeSendError{err}
	}
	if err := b.checkRegistration(ctx, reg); err != nil {
		return &BeforeSendError{err}
	}
	b.mu.Lock()
	c, err := b.carrierLocked(reg)
	if err == nil && c.draining {
		err = ErrDraining
	}
	if err == nil {
		if c.active == 0 {
			c.idle = make(chan struct{})
		}
		c.active++
	}
	b.mu.Unlock()
	if err != nil {
		return &BeforeSendError{err}
	}
	defer func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		c.active--
		if c.active == 0 {
			close(c.idle)
		}
		if c.draining && c.active == 0 {
			close(c.drained)
		}
	}()
	if err = b.registry.WithScopedLease(ctx, reg, prepare); err != nil {
		return &BeforeSendError{err}
	}
	if err = ctx.Err(); err != nil {
		return &BeforeSendError{err}
	}
	return send()
}

// WaitIdle waits for already-entered sends without disabling the stop and
// inspection sends needed to retire the carrier. The caller must first commit
// a backend maintenance gate so later business sends cannot reach transport.
func (b *Outbound) WaitIdle(ctx context.Context, reg Registration) error {
	if err := b.checkRegistration(ctx, reg); err != nil {
		return err
	}
	b.mu.Lock()
	c, err := b.carrierLocked(reg)
	var idle <-chan struct{}
	if err == nil {
		idle = c.idle
	}
	b.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Drain permanently closes this incarnation to new sends, including when its
// waiting context expires. Receive/event-commit paths remain available. A fresh
// carrier requires a new incarnation; a timeout must never reopen the old one.
func (b *Outbound) Drain(ctx context.Context, reg Registration) error {
	if err := b.checkRegistration(ctx, reg); err != nil {
		return err
	}
	b.mu.Lock()
	c, err := b.carrierLocked(reg)
	if err == nil && !c.draining {
		c.draining = true
		if c.active == 0 {
			close(c.drained)
		}
	}
	b.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-c.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Retire/Transfer are the host integration entry points, after durable drain
// intent has been recorded. The trusted verifier still proves native/background
// quiescence; draining socket writes alone is not execution completion.
func (b *Outbound) Retire(ctx context.Context, reg Registration, evidence string) error {
	if !canonical(evidence) {
		return fmt.Errorf("stop evidence reference required")
	}
	if err := b.Drain(ctx, reg); err != nil {
		return err
	}
	return b.registry.Retire(ctx, reg, evidence)
}

func (b *Outbound) Transfer(ctx context.Context, reg Registration, next Owner, evidence string) error {
	if err := next.validate(); err != nil {
		return err
	}
	if next.Epoch != reg.Owner.Epoch+1 || !canonical(evidence) {
		return fmt.Errorf("transfer requires next epoch and evidence")
	}
	if err := b.Drain(ctx, reg); err != nil {
		return err
	}
	return b.registry.Transfer(ctx, reg, next, evidence)
}
