package privatechat

import (
	"database/sql"
	"fmt"
)

// Rebuild without renaming the old table: dependent foreign-key declarations
// continue to reference dispatch. The application lock excludes other writers.
func migrateDispatchSchema7(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF; BEGIN IMMEDIATE`); err != nil {
		return err
	}
	indexes, err := db.Query(`SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name='dispatch' AND sql IS NOT NULL`)
	if err != nil {
		_, _ = db.Exec(`ROLLBACK; PRAGMA foreign_keys=ON`)
		return err
	}
	var definitions []string
	for indexes.Next() {
		var definition string
		if err = indexes.Scan(&definition); err != nil {
			indexes.Close()
			_, _ = db.Exec(`ROLLBACK; PRAGMA foreign_keys=ON`)
			return err
		}
		definitions = append(definitions, definition)
	}
	err = indexes.Err()
	closeErr := indexes.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_, _ = db.Exec(`ROLLBACK; PRAGMA foreign_keys=ON`)
		return err
	}
	if _, err := db.Exec(`CREATE TABLE dispatch_v7 (id TEXT PRIMARY KEY, gateway TEXT NOT NULL, tenant TEXT NOT NULL, app_id TEXT NOT NULL, surface TEXT NOT NULL, chat TEXT NOT NULL, user_id TEXT NOT NULL, generation INTEGER NOT NULL, lane TEXT NOT NULL CHECK(lane IN ('input','control','inspect')), mode TEXT NOT NULL, backend TEXT NOT NULL, namespace TEXT NOT NULL, session TEXT NOT NULL, carrier TEXT NOT NULL, carrier_generation INTEGER NOT NULL, command_id TEXT NOT NULL UNIQUE, state TEXT NOT NULL CHECK(state IN ('prepared','sending','sent','resolved','accepted','unknown','failed')), evidence BLOB, request_id TEXT NOT NULL DEFAULT '', event_sequence INTEGER NOT NULL DEFAULT 0);
 INSERT INTO dispatch_v7(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state,evidence,request_id,event_sequence) SELECT id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state,evidence,request_id,event_sequence FROM dispatch;
 DROP TABLE dispatch;
 ALTER TABLE dispatch_v7 RENAME TO dispatch;
 PRAGMA user_version=7;`); err != nil {
		_, _ = db.Exec(`ROLLBACK; PRAGMA foreign_keys=ON`)
		return fmt.Errorf("migrate private dispatch schema: %w", err)
	}
	for _, definition := range definitions {
		if _, err = db.Exec(definition); err != nil {
			_, _ = db.Exec(`ROLLBACK; PRAGMA foreign_keys=ON`)
			return fmt.Errorf("restore dispatch index: %w", err)
		}
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		_, _ = db.Exec(`ROLLBACK; PRAGMA foreign_keys=ON`)
		return err
	}
	bad := rows.Next()
	rowErr := rows.Err()
	closeErr = rows.Close()
	if rowErr == nil {
		rowErr = closeErr
	}
	if rowErr != nil {
		_, _ = db.Exec(`ROLLBACK; PRAGMA foreign_keys=ON`)
		return rowErr
	}
	if bad {
		_, _ = db.Exec(`ROLLBACK; PRAGMA foreign_keys=ON`)
		return fmt.Errorf("private dispatch migration violates foreign keys")
	}
	if _, err = db.Exec(`COMMIT; PRAGMA foreign_keys=ON`); err != nil {
		_, _ = db.Exec(`ROLLBACK; PRAGMA foreign_keys=ON`)
		return err
	}
	return nil
}
