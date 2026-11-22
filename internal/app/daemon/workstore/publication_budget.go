package workstore

import (
	"database/sql"
	"errors"
	"time"
)

// Persisted independently of Bot identity: all identities share a destination's
// background display window, including after daemon restart.
const SchemaPublicationBudgetV1 = `CREATE TABLE IF NOT EXISTS team_publication_budget (
 budget_key TEXT PRIMARY KEY, until_ns INTEGER NOT NULL
);`

func (t *Tx) PublicationBudgetUntil(key string) (time.Time, error) {
	var ns int64
	err := t.tx.QueryRowContext(t.ctx, "SELECT until_ns FROM team_publication_budget WHERE budget_key=?", key).Scan(&ns)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, ns), nil
}

func (t *Tx) SetPublicationBudget(key string, until time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO team_publication_budget(budget_key,until_ns) VALUES(?,?) ON CONFLICT(budget_key) DO UPDATE SET until_ns=MAX(until_ns,excluded.until_ns)`, key, until.UnixNano())
	return err
}
