package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type validatable interface{ Validate() error }

// record is an internal storage primitive. Every caller is a typed method
// that performs ownership/state checks before reaching this conditional write.
func (t *Tx) record(table, id string, revision, expected int64, value validatable) error {
	if err := value.Validate(); err != nil {
		return fmt.Errorf("%s: %w", table, err)
	}
	if expected < 0 || revision != expected+1 {
		return ErrConflict
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if expected == 0 {
		result, err := t.tx.ExecContext(t.ctx, "INSERT INTO "+table+"(id,revision,payload) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING", id, revision, string(payload))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		return nil
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE "+table+" SET revision=?,payload=? WHERE id=? AND revision=?", revision, string(payload), id, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (t *Tx) read(table, id string, destination any) error {
	var payload string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM "+table+" WHERE id=?", id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(payload), destination)
}
func listRecords[T any](t *Tx, table, where string, args ...any) ([]T, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM "+table+where+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []T{}
	for rows.Next() {
		var payload string
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		var value T
		if err = json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (t *Tx) exists(table, id string) error {
	var one int
	err := t.tx.QueryRowContext(t.ctx, "SELECT 1 FROM "+table+" WHERE id=?", id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func immutable(changed bool) error {
	if changed {
		return fmt.Errorf("immutable work ownership or binding changed")
	}
	return nil
}
