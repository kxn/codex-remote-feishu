package workengine

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestControlReserveKeepsBusinessParallelAndHonorsTotalOccupancy(t *testing.T) {
	s, _ := fixture(t, "a", "b", "c")
	for _, scope := range []string{"a", "b", "c"} {
		enqueue(t, s, "input-"+scope, scope, "leader", "", "")
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		context := work.WorkContext{ID: "reserved-control", ScopeID: "a", RoleID: "leader", Purpose: work.PurposeControl, Workstream: "reserved-control", Revision: 1}
		if e := tx.PutContext(context, 0); e != nil {
			return e
		}
		location := work.ExecutionLocation{ID: "reserved-scratch", HostID: "local", Cwd: "/scratch/reserved-control", IsolatedContextID: context.ID, Generation: 1, Revision: 1}
		if e := tx.PutLocation(location, 0); e != nil {
			return e
		}
		worker := work.WorkerSession{ID: "reserved-worker", ContextID: context.ID, ScopeID: "a", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "reserved-instance", CarrierIncarnation: "reserved-carrier", InstanceGeneration: 1, ProcessGeneration: 1, LocationID: location.ID, LocationGeneration: 1, Revision: 1}
		if e := tx.PutWorker(worker, 0); e != nil {
			return e
		}
		content := work.NewContent("reserve-human-body", "a", "text/plain", []byte("解释当前工作为何等待"), testNow)
		if e := tx.PutContent(content); e != nil {
			return e
		}
		message := work.Message{ID: "reserve-human", ScopeID: "a", ConversationID: "group", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, BodyRef: content.ID, Revision: 1}
		original := work.Inbox{ID: "reserve-business", ScopeID: "a", RecipientRoleID: "leader", EventID: "reserve-business", MessageID: message.ID, State: work.InboxPending, Revision: 1, AvailableAt: testNow}
		if _, _, e := tx.AcceptMessage(message, []work.Inbox{original}); e != nil {
			return e
		}
		return tx.PutInbox(work.Inbox{ID: "reserve-control-input", ScopeID: "a", RecipientRoleID: "leader", RecipientContextID: context.ID, ControlOfInputID: original.ID, MessageID: message.ID, EventID: "reserve-control", State: work.InboxPending, Revision: 1, AvailableAt: testNow}, 0)
	})
	e, err := New(s, Config{MaxConcurrent: 3, ControlReservedSlots: 1, Now: func() time.Time { return testNow }}, &testPlanner{}, &testDispatcher{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.Tick(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Dispatched) != 3 {
		t.Fatalf("expected two business plus control %+v", result)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		total, e := tx.ActiveSlotCount()
		if e != nil {
			return e
		}
		business, e := tx.ActiveBusinessSlotCount()
		if e != nil {
			return e
		}
		if total != 3 || business != 2 {
			t.Fatalf("reservation not enforced total=%d business=%d", total, business)
		}
		return nil
	})
	again, err := e.Tick(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Dispatched) != 0 {
		t.Fatal("uncertain occupancy was bypassed to admit more work")
	}
	for _, reserve := range []int{-1, 3, 4} {
		if _, err := New(s, Config{MaxConcurrent: 3, ControlReservedSlots: reserve}, &testPlanner{}, &testDispatcher{}); err == nil {
			t.Fatalf("invalid reserve accepted %d", reserve)
		}
	}
}
