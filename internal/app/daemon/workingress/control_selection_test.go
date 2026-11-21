package workingress

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestPausedProjectKeepsBusinessInputAndClaimsOnlyIsolatedHumanControl(t *testing.T) {
	i, s, _ := ingressFixture(t, true)
	txRun(t, s, func(tx *workstore.Tx) error {
		project, e := tx.GetProject("project")
		if e != nil {
			return e
		}
		project.State = work.ProjectPausing
		project.Revision++
		if e = tx.PutProject(project, 1); e != nil {
			return e
		}
		return tx.PutPauseGate(work.PauseGate{ID: "project-pause", ScopeID: "scope", Source: work.GateProject, SourceID: project.ID, State: work.GateClosed, Revision: 1}, 0)
	})
	receive(t, i, rawMessage("expert", "resume-input", "group", "group", "human-open", "human-user", "继续项目"))
	process(t, i)
	txRun(t, s, func(tx *workstore.Tx) error {
		inputs, e := tx.ListInbox("scope")
		if e != nil {
			return e
		}
		var control work.Inbox
		for _, input := range inputs {
			if input.ControlOfInputID != "" {
				control = input
			}
		}
		if len(inputs) != 2 || control.ID == "" {
			t.Fatalf("expected linked business+control inputs: %+v", inputs)
		}
		original, e := tx.GetInbox(control.ControlOfInputID)
		if e != nil {
			return e
		}
		if original.State != work.InboxPending || original.MessageID != control.MessageID {
			t.Fatal("business input discarded or forged")
		}
		context, e := tx.GetContext(control.RecipientContextID)
		if e != nil {
			return e
		}
		if context.Purpose != work.PurposeControl || context.RoleID != "leader" {
			t.Fatal("control not owned by actual leader")
		}
		worker := work.WorkerSession{ID: "control-worker", ContextID: context.ID, ScopeID: "scope", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "control-carrier", InstanceGeneration: 1, ProcessGeneration: 1, LocationID: "scratch", LocationGeneration: 1, Revision: 1}
		location := work.ExecutionLocation{ID: "scratch", HostID: "local", Cwd: "/scratch/control", Generation: 1, Revision: 1}
		if e = tx.PutLocation(location, 0); e != nil {
			return e
		}
		if e = tx.PutWorker(worker, 0); e != nil {
			return e
		}
		attempt := work.ExecutionAttempt{ID: "control-attempt", ScopeID: "scope", ContextID: context.ID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "control-run"}, InputIDs: []string{control.ID}, CommandID: "control-command", State: work.AttemptDispatchPending, Revision: 1}
		command := work.Operation{ID: attempt.CommandID, ScopeID: "scope", ContextID: context.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: "control-command", RequestHash: "hash", State: work.OperationPrepared, Revision: 1}
		claim := workstore.ClaimRequest{Attempt: attempt, StartOperation: command, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, Now: ingressNow}
		if e = tx.ClaimAttempt(claim); e == nil {
			t.Fatal("control admitted ordinary business location")
		}
		location.IsolatedContextID = context.ID
		location.Revision++
		if e = tx.PutLocation(location, 1); e != nil {
			return e
		}
		if e = tx.PutPauseGate(work.PauseGate{ID: "human-control", ScopeID: "scope", ContextID: context.ID, Source: work.GateHumanControl, SourceID: "human", State: work.GateClosed, Revision: 1}, 0); e != nil {
			return e
		}
		if e = tx.ClaimAttempt(claim); e == nil {
			t.Fatal("control bypassed human gate")
		}
		gate, e := tx.GetPauseGate("human-control")
		if e != nil {
			return e
		}
		gate.State = work.GateOpen
		gate.Revision++
		if e = tx.PutPauseGate(gate, 1); e != nil {
			return e
		}
		return tx.ClaimAttempt(claim)
	})
}
func TestBootstrapLeaderChoiceRequiresOriginalHumanCardAndReplaysOriginalInput(t *testing.T) {
	i, s, d := ingressFixture(t, false)
	d.leader = ""
	d.leaderErr = errors.New("ambiguous")
	receipt := receive(t, i, rawMessage("leader", "original", "group", "group", "owner-open", "owner-user", "建立这个项目"))
	if _, e := i.ProcessPending(t.Context(), 100); e != nil {
		t.Fatal(e)
	}
	var req workstore.SelectBootstrapLeaderRequest
	txRun(t, s, func(tx *workstore.Tx) error {
		gateway := d.gateways["gateway:leader"]
		notice, _, e := tx.TryBeginSelectionNotice(workstore.BeginSelectionNoticeRequest{IngressID: receipt.IngressID, Gateway: gateway, Text: "选择负责人", CandidateRoleIDs: []string{"leader", "expert"}, Now: ingressNow})
		if e != nil {
			return e
		}
		if e = tx.FinishSelectionNotice(notice.IngressID, notice.Revision, workstore.SelectionNoticeOutcome{State: work.OperationUnknown, LastError: "connection lost", Now: ingressNow}); e != nil {
			return e
		}
		raw := work.HumanInteractionEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "choice", Tenant: "tenant", ChatID: "group", MessageID: "actual-choice-card", Kind: "card", SenderIDs: work.SenderIDs{OpenID: "impostor-open", UserID: "impostor-user"}, Values: map[string]any{"team_setup_ingress": receipt.IngressID, "ingress_id": receipt.IngressID, "role_id": "expert", "binding_generation": "0"}, ReceivedAt: ingressNow}
		action, _, e := tx.AcceptHumanInteraction(raw, gateway)
		if e != nil {
			return e
		}
		req = workstore.SelectBootstrapLeaderRequest{IngressID: receipt.IngressID, RoleID: "expert", InteractionID: action.ID}
		if _, e = tx.SelectBootstrapLeader(req); e == nil {
			t.Fatal("non-establishing human selected leader")
		}
		raw.EventID = "actual-choice"
		raw.SenderIDs = work.SenderIDs{OpenID: "owner-open", UserID: "owner-user"}
		action, _, e = tx.AcceptHumanInteraction(raw, gateway)
		if e != nil {
			return e
		}
		req.InteractionID = action.ID
		if _, e = tx.SelectBootstrapLeader(req); e != nil {
			return e
		}
		notice, e = tx.GetSelectionNotice(receipt.IngressID)
		if e != nil {
			return e
		}
		if notice.State != work.OperationConfirmed || notice.ExternalMessageID != raw.MessageID {
			t.Fatal("real callback did not reconcile unknown notice")
		}
		return nil
	})
	process(t, i)
	txRun(t, s, func(tx *workstore.Tx) error {
		scope, e := tx.FindBootstrapScope("tenant", "group", 0)
		if e != nil {
			return e
		}
		if scope.RoleID != "expert" {
			t.Fatal("choice not used")
		}
		record, e := tx.GetIngress(receipt.IngressID)
		if e != nil {
			return e
		}
		if record.State != work.IngressProcessed || record.MessageID == "" {
			t.Fatal("original demand lost after leader selection")
		}
		_, e = tx.SelectBootstrapLeader(req)
		return e
	})
}
