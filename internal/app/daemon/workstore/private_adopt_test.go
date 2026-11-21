package workstore

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateInstructionAdoptionFreezesDedicatedTaskContext(t *testing.T) {
	s, path := openTestStore(t)
	seedPersonal(t, s)
	now := time.Now().UTC()
	var current PrivateInstructionAdoption
	requireTx(t, s, func(tx *Tx) error {
		gateway := work.IngressGateway{GatewayRef: "g", AppID: "app", RoleID: "r", BotGeneration: 1}
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "g", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "group", Purpose: "main", BindingGeneration: 1, Active: true}, 0); e != nil {
			return e
		}
		raw := work.RawIngressEnvelope{GatewayRef: "g", AppID: "app", EventID: "group-event", Tenant: "tenant", ChatID: "group", ChatType: "group", MessageID: "group-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "open"}, MessageType: "text", RawContent: `{"text":"hello"}`, MessageCreatedAt: now, ReceivedAt: now}
		record, _, err := tx.AcceptRawIngress(raw, gateway)
		if err != nil {
			return err
		}
		principal, _, err := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: record.ID})
		if err != nil {
			return err
		}
		if err = tx.GrantProjectAccessFromIngress("s", principal.ID, record.ID, now.Add(time.Hour)); err != nil {
			return err
		}
		personal := work.WorkScope{ID: "personal-adopt", Kind: work.ScopePersonal, RoleID: "r", PrincipalID: principal.ID, Revision: 1}
		if err = tx.PutScope(personal, 0); err != nil {
			return err
		}
		front := work.WorkContext{ID: "private-front", ScopeID: personal.ID, RoleID: "r", Workstream: "private", ProjectViewScopeID: "s", ProjectContextID: "c-r", Revision: 1, CreatedAt: now, UpdatedAt: now}
		if err = tx.PutContext(front, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "private-worker", ContextID: front.ID, ScopeID: personal.ID, RoleID: "r", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "private-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err = tx.PutWorker(worker, 0); err != nil {
			return err
		}
		body := work.NewContent("private-body", personal.ID, "text/plain", []byte("Please adopt this in the project. Keep my notes private."), now)
		if err = tx.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: "private-adopt-message", ScopeID: personal.ID, ConversationID: front.Workstream, Source: work.Actor{Kind: work.ActorPrincipal, ID: principal.ID}, BodyRef: body.ID, Revision: 1}
		inbox := work.Inbox{ID: "private-adopt-input", ScopeID: personal.ID, RecipientRoleID: "r", RecipientContextID: front.ID, EventID: "private-adopt-event", State: work.InboxPending, Revision: 1}
		if _, _, err = tx.AcceptMessage(message, []work.Inbox{inbox}); err != nil {
			return err
		}
		claim := testClaim()
		claim.Attempt.ContextID, claim.Attempt.ScopeID = front.ID, personal.ID
		claim.Attempt.RunRef.WorkerID = worker.ID
		claim.Attempt.InputIDs = []string{inbox.ID}
		claim.InstanceID = worker.InstanceID
		claim.StartOperation.ScopeID, claim.StartOperation.ContextID = personal.ID, front.ID
		claim.StartOperation.TargetID = worker.ID
		if err = tx.ClaimAttempt(claim); err != nil {
			return err
		}
		claim.Attempt.State = work.AttemptRunning
		claim.Attempt.Revision++
		if err = tx.PutAttempt(claim.Attempt, 1); err != nil {
			return err
		}
		req := AdoptPrivateInstructionRequest{Key: "adopt", Source: PrivateActionSource{PersonalScopeID: personal.ID, MessageID: message.ID, RunRef: claim.Attempt.RunRef}, Title: "Export", Summary: "Implement export.", Now: now}
		adopted, created, err := tx.AdoptPrivateInstruction(req)
		if err != nil || !created {
			t.Fatalf("adoption %+v %v", adopted, err)
		}
		replayed, created, err := tx.AdoptPrivateInstruction(req)
		if err != nil || created || replayed != adopted {
			t.Fatalf("replay %+v %v", replayed, err)
		}
		context, err := tx.GetContext(adopted.ProjectContextID)
		if err != nil {
			return err
		}
		projectInput, err := tx.GetInbox(adopted.InboxID)
		if err != nil {
			return err
		}
		if context.ID == front.ProjectContextID || context.TaskID != adopted.TaskID || projectInput.TaskID != adopted.TaskID || projectInput.RecipientContextID != context.ID {
			t.Fatalf("task and recipient context disagree: context=%+v input=%+v", context, projectInput)
		}
		current = adopted
		projectInput.RecipientContextID = front.ProjectContextID
		projectInput.Revision++
		if err = tx.record("inbox", projectInput.ID, projectInput.Revision, projectInput.Revision-1, projectInput); err != nil {
			return err
		}
		adopted.ProjectContextID = front.ProjectContextID
		_, err = tx.tx.ExecContext(tx.ctx, "UPDATE private_instruction_adoptions SET payload=? WHERE id=?", mustJSON(adopted), adopted.ID)
		return err
	})
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		input, err := tx.GetInbox(current.InboxID)
		if err == nil && (input.RecipientContextID != current.ProjectContextID || input.TaskID != current.TaskID || input.Revision != 3) {
			t.Fatalf("pending legacy adoption was not repaired: %+v", input)
		}
		return err
	})
}
