package workengine

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestPausedProjectSchedulesOnlyLinkedIsolatedLeaderControl(t *testing.T) {
	s, _ := fixture(t, "a")
	transaction(t, s, func(tx *workstore.Tx) error {
		project, e := tx.GetProject("project-a")
		if e != nil {
			return e
		}
		project.State = work.ProjectPausing
		project.Revision++
		if e = tx.PutProject(project, 1); e != nil {
			return e
		}
		if e = tx.PutPauseGate(work.PauseGate{ID: "pause", ScopeID: "a", Source: work.GateProject, SourceID: project.ID, State: work.GateClosed, Revision: 1}, 0); e != nil {
			return e
		}
		context := work.WorkContext{ID: "control", ScopeID: "a", RoleID: "leader", Purpose: work.PurposeControl, Workstream: "control", Revision: 1}
		if e = tx.PutContext(context, 0); e != nil {
			return e
		}
		location := work.ExecutionLocation{ID: "control-scratch", HostID: "local", Cwd: "/scratch/control", IsolatedContextID: context.ID, Generation: 1, Revision: 1}
		if e = tx.PutLocation(location, 0); e != nil {
			return e
		}
		if e = tx.PutWorker(work.WorkerSession{ID: "control-worker", ContextID: context.ID, ScopeID: "a", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, CarrierIncarnation: "control-carrier", InstanceID: "control-instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationID: location.ID, LocationGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		body := work.NewContent("human-body", "a", "text/plain", []byte("continue project"), testNow)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		message := work.Message{ID: "human-message", ScopeID: "a", ConversationID: "group", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, BodyRef: body.ID, Revision: 1}
		original := work.Inbox{ID: "business", ScopeID: "a", RecipientRoleID: "leader", EventID: "business", MessageID: message.ID, State: work.InboxPending, Revision: 1, AvailableAt: testNow}
		if _, _, e = tx.AcceptMessage(message, []work.Inbox{original}); e != nil {
			return e
		}
		control := work.Inbox{ID: "control-input", ScopeID: "a", RecipientRoleID: "leader", RecipientContextID: context.ID, ControlOfInputID: original.ID, MessageID: message.ID, EventID: "control", State: work.InboxPending, Revision: 1, AvailableAt: testNow}
		return tx.PutInbox(control, 0)
	})
	e := engine(t, s, 3, &testPlanner{}, &testDispatcher{})
	result, err := e.Tick(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Dispatched) != 1 {
		t.Fatalf("control scheduling %+v", result)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		attempt, e := tx.GetAttempt(result.Dispatched[0].AttemptID)
		if e != nil {
			return e
		}
		if attempt.ContextID != "control" || len(attempt.InputIDs) != 1 || attempt.InputIDs[0] != "control-input" {
			t.Fatalf("business pause bypassed: %+v", attempt)
		}
		input, e := tx.GetInbox("business")
		if e != nil {
			return e
		}
		if input.State != work.InboxPending {
			t.Fatal("business input consumed by management run")
		}
		return nil
	})
}
