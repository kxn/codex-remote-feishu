package workingress

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestActiveProjectRuntimeGateStillCreatesRestrictedManagementInput(t *testing.T) {
	i, s, _ := ingressFixture(t, true)
	txRun(t, s, func(tx *workstore.Tx) error {
		context := work.WorkContext{ID: "blocked-business", ScopeID: "scope", RoleID: "leader", Workstream: "business", Revision: 1}
		if e := tx.PutContext(context, 0); e != nil {
			return e
		}
		worker := work.WorkerSession{ID: "blocked-worker", ScopeID: "scope", ContextID: context.ID, RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerRecovering, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "blocked-instance", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if e := tx.PutWorker(worker, 0); e != nil {
			return e
		}
		return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: "native_goal_budget_exhausted", RecoveryReason: "native_goal_budget_exhausted", CreatedAt: ingressNow})
	})
	receive(t, i, rawMessage("leader", "continue", "group", "group", "human-open", "human-user", "怎么了，继续项目"))
	process(t, i)
	txRun(t, s, func(tx *workstore.Tx) error {
		project, e := tx.GetProject("project")
		if e != nil {
			return e
		}
		if project.State != work.ProjectActive {
			t.Fatal("management copy paused project or pretended recovery")
		}
		inputs, e := tx.ListInbox("scope")
		if e != nil {
			return e
		}
		if len(inputs) != 2 {
			t.Fatalf("blocked human lost management path: %+v", inputs)
		}
		for _, input := range inputs {
			if input.State != work.InboxPending {
				t.Fatal("new input was consumed")
			}
			if input.ControlOfInputID != "" {
				context, e := tx.GetContext(input.RecipientContextID)
				if e != nil {
					return e
				}
				if context.Purpose != work.PurposeControl || context.RoleID != project.LeaderRoleID {
					t.Fatal("not restricted leader control")
				}
			}
		}
		gate, e := tx.GetRuntimeGate("blocked-worker")
		if e != nil {
			return e
		}
		if gate.RecoveryReason != "native_goal_budget_exhausted" {
			t.Fatal("budget gate was cleared")
		}
		return nil
	})
}
func TestBlockedPersonalMessageStaysInOriginalConversation(t *testing.T) {
	i, s, _ := ingressFixture(t, false)
	_, nav := privateCommandSource(t, i, s, "command")
	txRun(t, s, func(tx *workstore.Tx) error {
		return tx.PutPauseGate(work.PauseGate{ID: "personal-pause", ScopeID: nav.PersonalScopeID, ContextID: nav.FrontContextID, Source: work.GateContext, SourceID: nav.PrincipalID, State: work.GateClosed, Revision: 1}, 0)
	})
	receive(t, i, rawMessage("leader", "question", "dm", "p2p", "open-person", "person", "hi"))
	process(t, i)
	txRun(t, s, func(tx *workstore.Tx) error {
		inputs, e := tx.ListInbox(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		if len(inputs) != 1 {
			t.Fatalf("ordinary private input spawned extra conversation: %+v", inputs)
		}
		input := inputs[0]
		if input.ControlOfInputID != "" || input.RecipientContextID != nav.FrontContextID || input.State != work.InboxPending {
			t.Fatalf("private input changed owner: %+v", input)
		}
		gate, e := tx.GetPauseGate("personal-pause")
		if e == nil && gate.State != work.GateClosed {
			t.Fatal("input fabricated recovery")
		}
		return e
	})
}

func TestLegacyPrivateManagementCopyRetiresWithoutConsumingOriginal(t *testing.T) {
	i, s, _ := ingressFixture(t, false)
	_, nav := privateCommandSource(t, i, s, "command")
	receive(t, i, rawMessage("leader", "hi", "dm", "p2p", "open-person", "person", "hi"))
	process(t, i)
	txRun(t, s, func(tx *workstore.Tx) error {
		inputs, err := tx.ListInbox(nav.PersonalScopeID)
		if err != nil {
			return err
		}
		if len(inputs) != 1 {
			t.Fatalf("inputs=%+v", inputs)
		}
		original := inputs[0]
		control := work.WorkContext{ID: "legacy-private-control", ScopeID: nav.PersonalScopeID, RoleID: original.RecipientRoleID, Purpose: work.PurposeControl, ControlTargetContextID: nav.FrontContextID, Workstream: "legacy-control", Revision: 1}
		if err = tx.PutContext(control, 0); err != nil {
			return err
		}
		copy := original
		copy.ID, copy.EventID = "legacy-copy", "legacy-copy-event"
		copy.RecipientContextID, copy.ControlOfInputID = control.ID, original.ID
		if err = tx.PutInbox(copy, 0); err != nil {
			return err
		}
		if err = tx.CancelObsoletePrivateControlInputs(); err != nil {
			return err
		}
		if err = tx.CancelObsoletePrivateControlInputs(); err != nil {
			return err
		}
		retired, err := tx.GetInbox(copy.ID)
		if err != nil {
			return err
		}
		if retired.State != work.InboxCancelled || retired.Revision != 2 {
			t.Fatalf("retirement=%+v", retired)
		}
		kept, err := tx.GetInbox(original.ID)
		if err == nil && (kept.State != work.InboxPending || kept.Revision != original.Revision || kept.RecipientContextID != nav.FrontContextID) {
			t.Fatalf("original changed: %+v", kept)
		}
		return err
	})
}
