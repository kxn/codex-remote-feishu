package executionowner

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"modernc.org/sqlite"
)

// localFence keeps acquisition cancellable but, once admitted, holds the
// registry transaction until the local-only callback and commit finish.
type localFence struct {
	parent context.Context
	ctx    context.Context
	cancel context.CancelFunc
	stop   func() bool
	tx     *sql.Tx
	conn   *sql.Conn
}

func (r *Registry) beginLocalFence(parent context.Context) (*localFence, error) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	f := &localFence{parent: parent, ctx: ctx, cancel: cancel}
	f.stop = context.AfterFunc(parent, cancel)
	conn, err := r.db.Conn(ctx)
	if err != nil {
		f.close()
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, err
	}
	f.conn = conn
	// modernc SQLite's busy handler can outlive a context deadline. Disable
	// it only while this exclusively checked-out connection acquires the
	// writer; the explicit retry below keeps lock acquisition cancellable.
	if _, err = conn.ExecContext(ctx, `PRAGMA busy_timeout=0`); err != nil {
		f.close()
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, err
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		f.tx, err = conn.BeginTx(ctx, nil)
		if err == nil {
			return f, nil
		}
		if parent.Err() != nil {
			f.close()
			return nil, parent.Err()
		}
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) || (sqliteErr.Code()&0xff != 5 && sqliteErr.Code()&0xff != 6) {
			f.close()
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-parent.Done():
			timer.Stop()
			f.close()
			return nil, parent.Err()
		case <-deadline.C:
			timer.Stop()
			f.close()
			return nil, err
		case <-timer.C:
		}
	}
}
func (f *localFence) enter() error {
	// A false stop means cancellation already started; never invoke the callback.
	if !f.stop() {
		if err := f.parent.Err(); err != nil {
			return err
		}
		return context.Canceled
	}
	if err := f.parent.Err(); err != nil {
		return err
	}
	// Acquisition is complete and cancellation is disarmed. Restore normal
	// SQLite contention behavior before running the callback and committing.
	_, err := f.tx.ExecContext(f.ctx, `PRAGMA busy_timeout=5000`)
	return err
}
func (f *localFence) close() {
	if f.tx != nil {
		_ = f.tx.Rollback()
	}
	if f.conn != nil {
		// Restore the registry's normal timeout before the pooled connection is
		// visible to unrelated operations again.
		if _, err := f.conn.ExecContext(context.Background(), `PRAGMA busy_timeout=5000`); err != nil {
			// Never return a connection with an unknown timeout to the pool.
			_ = f.conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = f.conn.Close()
	}
	f.stop()
	f.cancel()
}
