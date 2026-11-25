package workstore

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNativeMaintenanceQueriesUseStateIndexBeforeHistoricalPayload(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		for i := 0; i < 100; i++ {
			id := fmt.Sprintf("historical-inspection-%d", i)
			payload, _ := json.Marshal(map[string]any{"id": id, "scope_id": "s", "actor": map[string]string{"kind": "system", "id": "fixture"}, "kind": "runtime.inspect", "state": "confirmed", "idempotency_key": id, "large": strings.Repeat("x", 4096), "steps": 42})
			if _, err := tx.tx.ExecContext(tx.ctx, `INSERT INTO operations(id,revision,payload) VALUES(?,1,?)`, id, string(payload)); err != nil {
				return err
			}
		}
		for _, v := range []struct {
			id, kind, state, target string
			generation              int
		}{
			{"launch-old", "worker.launch", "confirmed", "worker", 1},
			{"launch-current", "worker.launch", "unknown", "worker", 2},
			{"rebuild-old", "worker.rebuild", "confirmed", "worker", 1},
			{"compact-current", "worker.compact.rebuild", "prepared", "worker", 2},
			{"compact-other", "worker.compact.rebuild", "prepared", "other", 2},
		} {
			payload, _ := json.Marshal(map[string]any{"id": v.id, "scope_id": "s", "actor": map[string]string{"kind": "system", "id": "fixture"}, "kind": v.kind, "state": v.state, "idempotency_key": v.id, "target_id": v.target, "target_generation": v.generation})
			if _, err := tx.tx.ExecContext(tx.ctx, `INSERT INTO operations(id,revision,payload) VALUES(?,1,?)`, v.id, string(payload)); err != nil {
				return err
			}
		}
		collect := func(query string) ([]string, error) {
			rows, err := tx.tx.QueryContext(tx.ctx, query)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			var ids []string
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					return nil, err
				}
				ids = append(ids, id)
			}
			return ids, rows.Err()
		}
		oldIDs, err := collect(`SELECT id FROM operations WHERE json_extract(payload,'$.kind')='worker.compact.rebuild' AND json_extract(payload,'$.state') IN ('prepared','sending','unknown') AND json_extract(payload,'$.target_id')='worker' AND json_extract(payload,'$.target_generation')=2 ORDER BY id`)
		if err != nil {
			return err
		}
		newIDs, err := collect(`SELECT id FROM operations WHERE kind='worker.compact.rebuild' AND state IN ('prepared','sending','unknown') AND json_extract(payload,'$.target_id')='worker' AND json_extract(payload,'$.target_generation')=2 ORDER BY id`)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(oldIDs, newIDs) || !reflect.DeepEqual(newIDs, []string{"compact-current"}) {
			t.Fatalf("candidate semantics changed: old=%v new=%v", oldIDs, newIDs)
		}
		checks := []struct {
			name, query string
			args        []any
		}{
			{"startup", `EXPLAIN QUERY PLAN SELECT w.payload` + nativePoolPredicate + ` AND w.state IN ('preparing','recovering') AND EXISTS (SELECT 1 FROM carrier_registrations c WHERE c.incarnation=json_extract(w.payload,'$.carrier_incarnation') AND c.connected=0) ORDER BY json_extract(w.payload,'$.updated_at'),w.id`, []any{"local"}},
			{"compactions", `EXPLAIN QUERY PLAN ` + listHostCompactionsSQL, []any{"local", 16}},
			{"assert-available", `EXPLAIN QUERY PLAN SELECT count(*) FROM operations WHERE kind='worker.compact.rebuild' AND json_extract(payload,'$.target_id')=? AND json_extract(payload,'$.target_generation')=? AND state IN ('prepared','sending','unknown')`, []any{"worker", int64(1)}},
		}
		for _, c := range checks {
			rows, err := tx.tx.QueryContext(tx.ctx, c.query, c.args...)
			if err != nil {
				return err
			}
			indexed := false
			for rows.Next() {
				var a, b, d int
				var detail string
				if err = rows.Scan(&a, &b, &d, &detail); err != nil {
					rows.Close()
					return err
				}
				if strings.Contains(detail, "operations_pending") {
					indexed = true
				}
				if strings.Contains(detail, "SCAN o") || strings.Contains(detail, "SCAN operations") {
					t.Errorf("%s scans operations: %s", c.name, detail)
				}
			}
			if err = rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			if !indexed {
				t.Errorf("%s did not use operations state index", c.name)
			}
		}
		ops, err := tx.ListHostCompactions("local", 16)
		if err != nil {
			return err
		}
		if len(ops) != 0 {
			t.Fatalf("unrelated history selected: %+v", ops)
		}
		return nil
	})
}
