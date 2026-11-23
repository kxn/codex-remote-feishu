package executionowner

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// Lease is a capability for local application commits inside one registry
// transaction. It is supplied by WithScopedLease, cannot be manufactured by a
// caller, and expires before that transaction commits. It performs no network
// I/O and does not permit ownership changes or nested calls to the registry.
type Lease struct {
	mu           sync.Mutex
	registration Registration
	open         bool
	ctx          context.Context
	tx           *sql.Tx
}

func (l *Lease) Run(reg Registration, commit func() error) error {
	if l == nil || commit == nil {
		return fmt.Errorf("active execution lease required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.registration != reg {
		return ErrConflict
	}
	if err := l.ctx.Err(); err != nil {
		return err
	}
	return commit()
}

// WithScopedLease allows an application journal to use the already-held
// ownership transaction without recursively acquiring the registry DB lock.
// A successful local commit is durable even if the outer transaction later
// fails; callers must retain it as prepared/unknown, never treat it as sent.
func (r *Registry) WithScopedLease(ctx context.Context, reg Registration, commit func(*Lease) error) error {
	if commit == nil {
		return fmt.Errorf("lease callback required")
	}
	return r.withLeaseTx(ctx, reg, func(tx *sql.Tx) error {
		lease := &Lease{registration: reg, open: true, ctx: ctx, tx: tx}
		defer func() { lease.mu.Lock(); lease.open = false; lease.mu.Unlock() }()
		return commit(lease)
	})
}

// BindSession records only a session proven by a trusted native event committed
// during this lease. The caller must establish that provenance before invoking.
func (l *Lease) BindSession(reg Registration, session string) error {
	if l == nil {
		return fmt.Errorf("active execution lease required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.registration != reg || l.tx == nil {
		return ErrConflict
	}
	if err := l.ctx.Err(); err != nil {
		return err
	}
	return bindSessionTx(l.ctx, l.tx, reg, session)
}

// ReserveSession holds a requested resume identity during this scoped lease;
// a later verified native event must still establish the actual session.
func (l *Lease) ReserveSession(reg Registration, session string) error {
	if l == nil || !canonical(session) {
		return ErrConflict
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.registration != reg || l.tx == nil {
		return ErrConflict
	}
	if err := l.ctx.Err(); err != nil {
		return err
	}
	return reserveSessionTx(l.ctx, l.tx, reg, session)
}

// CheckBusiness fences new business dispatch during backend maintenance while
// keeping receipt projection and stop/inspect available on the same carrier.
func (l *Lease) CheckBusiness(reg Registration) error {
	if l == nil {
		return ErrConflict
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.registration != reg || l.tx == nil {
		return ErrConflict
	}
	if err := l.ctx.Err(); err != nil {
		return err
	}
	return checkMaintenanceTx(l.ctx, l.tx, reg.Carrier.Backend)
}
