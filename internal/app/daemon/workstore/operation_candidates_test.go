package workstore

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestOperationCandidatesFilterBeforeDecodeAndPreserveOrdering(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutScope(work.WorkScope{ID: "z", Kind: work.ScopeLibrary, Revision: 1}, 0); err != nil {
			return err
		}
		insert := func(id, scope, kind, state string, malformed bool) error {
			p := map[string]any{"id": id, "scope_id": scope, "actor": map[string]any{"kind": "system", "id": "test"}, "kind": kind, "idempotency_key": id, "state": state, "revision": 1, "steps": []any{}}
			if malformed {
				p["steps"] = 42
				p["large"] = strings.Repeat("x", 16384)
			}
			b, e := json.Marshal(p)
			if e != nil {
				return e
			}
			_, e = tx.tx.ExecContext(tx.ctx, `INSERT INTO operations(id,revision,payload) VALUES(?,1,?)`, id, string(b))
			return e
		}
		for i := 0; i < 300; i++ {
			if e := insert(fmt.Sprintf("unrelated-%03d", i), "s", "runtime.inspect", "confirmed", true); e != nil {
				return e
			}
		}
		for _, v := range [][4]string{{"b", "s", "native.goal.policy", "prepared"}, {"a", "z", "native.goal.policy", "prepared"}, {"a-stop", "s", "native.gate_stop", "prepared"}, {"b-stop", "s", "native.gate_stop", "sending"}, {"c-stop", "s", "native.gate_stop", "unknown"}, {"d-stop", "s", "native.gate_stop", "retryable"}, {"other-stop", "z", "native.gate_stop", "prepared"}} {
			if e := insert(v[0], v[1], v[2], v[3], false); e != nil {
				return e
			}
		}
		for _, state := range []string{"confirmed", "terminal"} {
			if e := insert("skip-"+state, "s", "native.gate_stop", state, true); e != nil {
				return e
			}
		}
		for _, v := range [][4]string{{"p-1", "s", "project.provision", "prepared"}, {"p-2", "s", "project.member.provision", "sending"}, {"p-3", "z", "project.provision", "retryable"}, {"p-skip", "s", "project.provision", "unknown"}} {
			if e := insert(v[0], v[1], v[2], v[3], true); e != nil {
				return e
			}
		}
		provisions, e := tx.PendingProjectProvisionIDs()
		if e != nil || !reflect.DeepEqual(provisions, []string{"p-1", "p-2", "p-3"}) {
			t.Fatalf("provisioning ids: %v, %v", provisions, e)
		}
		goals, e := tx.ListPreparedNativeGoalPolicies(1)
		if e != nil {
			return e
		}
		if len(goals) != 1 || goals[0].ID != "b" {
			t.Fatalf("goal order/limit: %+v", goals)
		}
		goals, e = tx.ListPreparedNativeGoalPolicies(5)
		if e != nil {
			return e
		}
		if len(goals) != 2 || goals[0].ID != "b" || goals[1].ID != "a" {
			t.Fatalf("goal scope order: %+v", goals)
		}
		stops, e := tx.ListUnfinishedNativeGateStops("s")
		if e != nil {
			return e
		}
		var ids []string
		for _, v := range stops {
			ids = append(ids, v.ID)
		}
		if !reflect.DeepEqual(ids, []string{"a-stop", "b-stop", "c-stop", "d-stop"}) {
			t.Fatalf("stops: %v", ids)
		}
		if _, e = tx.ListOperations("s"); e == nil {
			t.Fatal("expected old broad scan to decode malformed unrelated operation")
		}
		for _, query := range []string{`EXPLAIN QUERY PLAN SELECT payload FROM operations WHERE state='prepared' AND kind='native.goal.policy' ORDER BY scope_id,id LIMIT 32`, `EXPLAIN QUERY PLAN SELECT payload FROM operations WHERE state IN ('prepared','sending','unknown','retryable') AND kind='native.gate_stop' AND scope_id='s' ORDER BY id`, `EXPLAIN QUERY PLAN SELECT id FROM operations WHERE state IN ('prepared','sending','retryable') AND kind IN ('project.provision','project.member.provision') ORDER BY scope_id,id`} {
			rows, e := tx.tx.QueryContext(tx.ctx, query)
			if e != nil {
				return e
			}
			found := false
			for rows.Next() {
				var a, b, c int
				var detail string
				if e = rows.Scan(&a, &b, &c, &detail); e != nil {
					rows.Close()
					return e
				}
				found = found || strings.Contains(detail, "operations_pending")
			}
			if e = rows.Err(); e != nil {
				rows.Close()
				return e
			}
			if e = rows.Close(); e != nil {
				return e
			}
			if !found {
				t.Errorf("state index not used: %s", query)
			}
		}
		return nil
	})
}

func TestOperationCandidateQueryCostOnLargeHistory(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		for i := 0; i < 500; i++ {
			op := testOperation(fmt.Sprintf("history-%04d", i))
			op.Kind = "runtime.inspect"
			op.State = work.OperationConfirmed
			op.ExternalReceipt = strings.Repeat("x", 8192)
			b, e := json.Marshal(op)
			if e != nil {
				return e
			}
			if _, e = tx.tx.ExecContext(tx.ctx, `INSERT INTO operations(id,revision,payload) VALUES(?,1,?)`, op.ID, string(b)); e != nil {
				return e
			}
		}
		start := time.Now()
		for i := 0; i < 10; i++ {
			if _, e := tx.ListOperations("s"); e != nil {
				return e
			}
		}
		old := time.Since(start)
		start = time.Now()
		for i := 0; i < 10; i++ {
			if _, e := tx.ListUnfinishedNativeGateStops("s"); e != nil {
				return e
			}
			if _, e := tx.ListPreparedNativeGoalPolicies(32); e != nil {
				return e
			}
		}
		narrow := time.Since(start)
		t.Logf("500 confirmed records x 8KiB: 10 broad scans=%s; 10 pairs of filtered scans=%s", old, narrow)
		return nil
	})
}
