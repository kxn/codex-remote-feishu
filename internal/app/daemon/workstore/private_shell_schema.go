package workstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const legacyInboxAttemptCheck = "CHECK(state NOT IN ('claimed','processed') OR claimed_attempt_id IS NOT NULL)"
const privateShellInboxAttemptCheck = "CHECK(state NOT IN ('claimed','processed') OR claimed_attempt_id IS NOT NULL OR (state='processed' AND coalesce(json_extract(payload,'$.shell_operation_id'),'')<>''))"

// Open calls this before exposing the Store to any caller. SQLite requires
// foreign_keys OFF outside the rebuild transaction; deferral alone could run
// DROP TABLE cascades. Recreate the original schema and all explicit indexes /
// triggers, copy only writable columns, and verify every foreign key before
// commit. Rollback retains the original table and its dependent rows.
func (s *Store) migratePrivateShellInbox(ctx context.Context) (err error) {
	var original string
	if err = s.db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name='inbox'").Scan(&original); err != nil {
		return err
	}
	if strings.Contains(original, privateShellInboxAttemptCheck) {
		return nil
	}
	prefix := "CREATE TABLE inbox ("
	if strings.HasPrefix(original, `CREATE TABLE "inbox" (`) {
		prefix = `CREATE TABLE "inbox" (`
	}
	if !strings.Contains(original, legacyInboxAttemptCheck) || !strings.HasPrefix(original, prefix) {
		return fmt.Errorf("unsupported inbox schema for independent shell migration")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer func() {
		_, restore := conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys=ON")
		err = errors.Join(err, restore)
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT sql FROM sqlite_master WHERE tbl_name='inbox' AND type IN ('index','trigger') AND sql IS NOT NULL ORDER BY type,name")
	if err != nil {
		return err
	}
	var definitions []string
	for rows.Next() {
		var definition string
		if err = rows.Scan(&definition); err != nil {
			rows.Close()
			return err
		}
		definitions = append(definitions, definition)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	create := strings.Replace(original, prefix, "CREATE TABLE inbox_shell_migration (", 1)
	create = strings.Replace(create, legacyInboxAttemptCheck, privateShellInboxAttemptCheck, 1)
	for _, statement := range []string{create, "INSERT INTO inbox_shell_migration(id,revision,payload) SELECT id,revision,payload FROM inbox", "DROP TABLE inbox", "ALTER TABLE inbox_shell_migration RENAME TO inbox"} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, definition := range definitions {
		if _, err = tx.ExecContext(ctx, definition); err != nil {
			return err
		}
	}
	check, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	hasViolation := check.Next()
	scanErr := check.Err()
	check.Close()
	if scanErr != nil {
		return scanErr
	}
	if hasViolation {
		return fmt.Errorf("independent shell migration failed foreign key verification")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}
