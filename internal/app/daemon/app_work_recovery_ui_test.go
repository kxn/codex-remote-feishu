package daemon

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

func TestTeamRecoveryShowsBudgetStopAndRetiresControlsAfterGateClears(t *testing.T) {
	a := teamProjectViewFixture(t)
	worker := work.WorkerSession{ID: "worker", ContextID: "context", ScopeID: "scope", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
	txRun := func(fn func(*workstore.Tx) error) {
		t.Helper()
		if err := a.teamRuntime.store.Within(t.Context(), fn); err != nil {
			t.Fatal(err)
		}
	}
	txRun(func(tx *workstore.Tx) error {
		if err := tx.PutContext(work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "leader", Workstream: "main", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: "worker", ScopeID: "scope", Reason: "native_goal_budget_exhausted", CreatedAt: time.Now().UTC()})
	})
	if err := a.runTeamRuntimeViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	var original string
	txRun(func(tx *workstore.Tx) error {
		views, err := tx.ListUIViews("scope")
		if err != nil {
			return err
		}
		if len(views) != 1 {
			t.Fatalf("views=%d", len(views))
		}
		original = views[0].ID
		body, err := tx.GetContent("scope", views[0].BodyRef)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body.Data), "任务还没有完成") {
			t.Fatal("budget stop presented as ordinary completed work")
		}
		return tx.ClearRuntimeGate("worker", "native_goal_budget_exhausted")
	})
	for range 2 {
		if err := a.runTeamRuntimeViews(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	txRun(func(tx *workstore.Tx) error {
		old, err := tx.GetUIView(original)
		if err != nil {
			return err
		}
		if !old.Invalidated {
			t.Fatal("obsolete recovery controls remain active")
		}
		views, err := tx.ListUIViews("scope")
		if err != nil {
			return err
		}
		if len(views) != 2 {
			t.Fatalf("repeated tick created %d views", len(views))
		}
		for _, v := range views {
			if !v.Invalidated {
				content, e := tx.GetContent("scope", v.BodyRef)
				if e != nil {
					return e
				}
				var p workservice.MessageProjection
				if e = json.Unmarshal(content.Data, &p); e != nil {
					return e
				}
				if strings.Contains(string(p.Card), "runtime.recover") || strings.Contains(string(p.Card), "runtime.observe") {
					t.Fatal("retired status still offers stale controls")
				}
			}
		}
		return nil
	})
}
