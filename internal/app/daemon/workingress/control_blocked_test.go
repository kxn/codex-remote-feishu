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
func TestPausedPersonalFrontGetsItsOwnControlContextWithoutOpeningGate(t *testing.T) {
	i, s, _ := ingressFixture(t, false)
	_, nav := privateCommandSource(t, i, s, "command")
	txRun(t, s, func(tx *workstore.Tx) error {
		return tx.PutPauseGate(work.PauseGate{ID: "personal-pause", ScopeID: nav.PersonalScopeID, ContextID: nav.FrontContextID, Source: work.GateContext, SourceID: nav.PrincipalID, State: work.GateClosed, Revision: 1}, 0)
	})
	receive(t, i, rawMessage("leader", "question", "dm", "p2p", "open-person", "person", "为什么没继续"))
	process(t, i)
	txRun(t, s, func(tx *workstore.Tx) error {
		inputs, e := tx.ListInbox(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		var control *work.Inbox
		for _, input := range inputs {
			if input.ControlOfInputID != "" {
				copy := input
				control = &copy
			}
		}
		if control == nil {
			t.Fatal("personal stop stranded natural language input")
		}
		context, e := tx.GetContext(control.RecipientContextID)
		if e != nil {
			return e
		}
		if context.ScopeID != nav.PersonalScopeID || context.ControlTargetContextID != nav.FrontContextID || context.Purpose != work.PurposeControl {
			t.Fatal("management borrowed another front")
		}
		gate, e := tx.GetPauseGate("personal-pause")
		if e != nil {
			return e
		}
		if gate.State != work.GateClosed {
			t.Fatal("explanation automatically resumed work")
		}
		allowed, e := work.GatesAllowRunForContext(context.ScopeID, "", context.ID, []work.PauseGate{gate})
		if e != nil {
			return e
		}
		if !allowed {
			t.Fatal("paused business context blocked isolated explanation")
		}
		return nil
	})
}
