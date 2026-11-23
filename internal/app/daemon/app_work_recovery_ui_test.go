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

func TestPrivateIdleRecoverySuppressesCardsUntilInputIsStuck(t *testing.T) {
	for _, reason := range []string{"carrier_disconnected_reconcile", "snapshot_rebuild_required"} {
		t.Run(reason, func(t *testing.T) {
			a, source := teamPickerFixture(t)
			var worker work.WorkerSession
			now := time.Now().UTC()
			pickerTx(t, a, func(tx *workstore.Tx) error {
				nav, err := tx.GetPrivateNavigation(source.ScopeID)
				if err != nil {
					return err
				}
				worker = work.WorkerSession{ID: "idle-worker", ContextID: nav.FrontContextID, ScopeID: source.ScopeID, RoleID: source.RoleID, Backend: work.BackendCodex, State: work.WorkerRecovering, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, Revision: 1}
				if err = tx.PutWorker(worker, 0); err != nil {
					return err
				}
				if err = recoveryTestDestination(tx, source, worker.ContextID); err != nil {
					return err
				}
				return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: reason, CreatedAt: now.Add(-time.Minute)})
			})
			if err := a.runTeamRuntimeViews(t.Context()); err != nil {
				t.Fatal(err)
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				views, err := tx.ListUIViews(source.ScopeID)
				if err != nil {
					return err
				}
				if len(views) != 0 {
					t.Fatalf("idle recovery created %d views", len(views))
				}
				return tx.PutInbox(work.Inbox{ID: "pending", ScopeID: source.ScopeID, RecipientRoleID: source.RoleID, RecipientContextID: worker.ContextID, EventID: "pending-event", State: work.InboxPending, AvailableAt: now.Add(-time.Minute), Revision: 1}, 0)
			})
			if err := a.runTeamRuntimeViews(t.Context()); err != nil {
				t.Fatal(err)
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				views, err := tx.ListUIViews(source.ScopeID)
				if err != nil {
					return err
				}
				if len(views) != 1 || views[0].Kind != "runtime.worker" {
					t.Fatalf("stuck input views: %+v", views)
				}
				return nil
			})
		})
	}
}

func TestPrivateIdleRecoveryInvalidatesUnsentOldCardWithoutNewPublication(t *testing.T) {
	a, source := teamPickerFixture(t)
	pickerTx(t, a, func(tx *workstore.Tx) error {
		nav, err := tx.GetPrivateNavigation(source.ScopeID)
		if err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "idle-worker", ContextID: nav.FrontContextID, ScopeID: source.ScopeID, RoleID: source.RoleID, Backend: work.BackendCodex, State: work.WorkerRecovering, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, Revision: 1}
		if err = tx.PutWorker(worker, 0); err != nil {
			return err
		}
		if err = recoveryTestDestination(tx, source, worker.ContextID); err != nil {
			return err
		}
		gate := workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: "carrier_disconnected_reconcile", CreatedAt: time.Now().Add(-time.Minute)}
		if err = tx.PutRuntimeGate(gate); err != nil {
			return err
		}
		gate, err = tx.GetRuntimeGate(worker.ID)
		if err != nil {
			return err
		}
		_, err = a.queueTeamRecoveryView(tx, worker, gate, "")
		return err
	})
	if err := a.runTeamRuntimeViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		views, err := tx.ListUIViews(source.ScopeID)
		if err != nil {
			return err
		}
		if len(views) != 1 || !views[0].Invalidated {
			t.Fatalf("old pending card not retired: %+v", views)
		}
		return tx.PutInbox(work.Inbox{ID: "late-input", ScopeID: source.ScopeID, RecipientRoleID: source.RoleID, RecipientContextID: workerContextForTest(t, tx, source.ScopeID), EventID: "late-event", State: work.InboxPending, AvailableAt: time.Now().Add(-time.Minute), Revision: 1}, 0)
	})
	if err := a.runTeamRuntimeViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := a.runTeamRuntimeViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		views, err := tx.ListUIViews(source.ScopeID)
		if err != nil {
			return err
		}
		if len(views) != 2 || views[0].Invalidated == views[1].Invalidated {
			t.Fatalf("stuck late input did not create one fresh warning: %+v", views)
		}
		return nil
	})
}

func workerContextForTest(t *testing.T, tx *workstore.Tx, scope string) string {
	t.Helper()
	nav, err := tx.GetPrivateNavigation(scope)
	if err != nil {
		t.Fatal(err)
	}
	return nav.FrontContextID
}

func recoveryTestDestination(tx *workstore.Tx, source teamHumanSource, contextID string) error {
	now := time.Now().UTC()
	body := work.NewContent("destination-body", source.ScopeID, "text/plain", []byte("hello"), now)
	if err := tx.PutContent(body); err != nil {
		return err
	}
	message := work.Message{ID: "destination-message", ScopeID: source.ScopeID, ConversationID: source.ChatID, Source: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, PlatformKey: &work.MessageKey{Tenant: source.Tenant, ChatID: source.ChatID, MessageID: "destination-message"}, Observations: []work.Observation{{AppID: source.Gateway.AppID, IdentityGeneration: 1, ReceivedAt: now}}, BodyRef: body.ID, Revision: 1, CreatedAt: now}
	input := work.Inbox{ID: "destination-input", ScopeID: source.ScopeID, RecipientRoleID: source.RoleID, RecipientContextID: contextID, MessageID: message.ID, EventID: "destination-event", State: work.InboxPending, Revision: 1}
	if _, _, err := tx.AcceptMessage(message, []work.Inbox{input}); err != nil {
		return err
	}
	input, err := tx.GetInbox(input.ID)
	if err != nil {
		return err
	}
	input.State = work.InboxCancelled
	input.Revision++
	return tx.PutInbox(input, 1)
}
