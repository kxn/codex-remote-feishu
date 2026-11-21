package daemon

import (
	"context"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

func TestResourceRecoveryRejectsOutsiderAndChangedOriginalRevision(t *testing.T) {
	f := newResourceRecoveryFixture(t)
	id := f.unknown(t, "resource.document.create")
	if err := f.a.runTeamResourceWriteRecoveryViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	outsider := f.click(t, "resource.write.recovery", "resource.write.inspect", "outsider", map[string]any{"source_url": "https://a.feishu.cn/docx/chosen"})
	if _, err := f.a.teamRuntime.service.InspectResourceWriteRecovery(t.Context(), outsider.ID, outsider.Envelope.Values["team_view"].(string)); err == nil || f.connector.reads != 0 {
		t.Fatal("outsider adopted a source or used bound read credentials")
	}
	first := f.click(t, "resource.write.recovery", "resource.write.inspect", "owner", map[string]any{"source_url": "https://a.feishu.cn/docx/chosen"})
	viewID := first.Envelope.Values["team_view"].(string)
	inspection, err := f.a.teamRuntime.service.InspectResourceWriteRecovery(t.Context(), first.ID, viewID)
	if err != nil {
		t.Fatal(err)
	}
	var view workstore.UIView
	if err = f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error { var e error; view, e = tx.GetUIView(viewID); return e }); err != nil {
		t.Fatal(err)
	}
	if err = f.a.queueResourceRecoveryConfirmation(t.Context(), first, view, inspection); err != nil {
		t.Fatal(err)
	}
	second := f.click(t, "resource.write.recovery.confirm", "resource.write.adopt", "owner", nil)
	if err = f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		old := op.Revision
		op.Revision++
		op.LastError = "a later actual platform observation changed the original"
		_, _, e = tx.PutOperation(op, old)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.a.teamRuntime.service.AdoptResourceWriteRecovery(t.Context(), second.ID, second.Envelope.Values["team_view"].(string)); err == nil {
		t.Fatal("stale operation version adopted unseen later facts")
	}
}
func TestResourceRecoveryArchiveProofRejectsForgedAuditAndPureReadsKeepTheirState(t *testing.T) {
	f := newResourceRecoveryFixture(t)
	id := f.unknown(t, "resource.document.append")
	if err := f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		audit := work.Operation{ID: "forged-recovery", ScopeID: "scope", Actor: work.Actor{Kind: work.ActorRole, ID: "leader"}, Kind: "resource.write.adopt", TargetID: id, TargetGeneration: 1, IdempotencyKey: "forged", RequestHash: "fake", State: work.OperationConfirmed, ExternalReceipt: "fake", Revision: 1}
		if _, _, e = tx.PutOperation(audit, 0); e != nil {
			return e
		}
		old := op.Revision
		op.Revision++
		op.Steps = append(op.Steps, work.OperationStep{ID: "human_resource_recovery", State: work.OperationConfirmed, Receipt: audit.ID})
		if _, _, e = tx.PutOperation(op, old); e != nil {
			return e
		}
		valid, e := tx.ResourceWriteWasRecovered(op)
		if e == nil || valid {
			t.Fatal("role-authored fake human audit allowed archive")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	g := newResourceRecoveryFixture(t)
	if err := g.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		for i, kind := range []string{"resource.register", "resource.capture", "resource.comments.read", "resource.write.observe", "resource.write.inspect"} {
			op := work.Operation{ID: fmt.Sprint("read-", i), ScopeID: "scope", Actor: work.Actor{Kind: work.ActorRole, ID: "leader"}, Kind: kind, TargetID: "target", TargetGeneration: 1, IdempotencyKey: fmt.Sprint(i), RequestHash: "read-only", State: work.OperationSending, Revision: 1}
			if _, _, e := tx.PutOperation(op, 0); e != nil {
				return e
			}
		}
		project, e := tx.GetProject("project")
		if e != nil {
			return e
		}
		old := project.Revision
		project.State = work.ProjectArchiving
		project.Revision++
		return tx.PutProject(project, old)
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.a.teamRuntime.service.ReconcileProjectLifecycle(t.Context(), "project"); err != nil {
		t.Fatal(err)
	}
	if err := g.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		project, e := tx.GetProject("project")
		if e != nil {
			return e
		}
		if project.State != work.ProjectArchived {
			t.Fatal("read-only interrupted observations blocked archive")
		}
		ops, e := tx.ListOperations("scope")
		for _, op := range ops {
			if strings.HasPrefix(op.ID, "read-") && op.State != work.OperationSending {
				t.Fatal("archive fabricated read completion/cancellation")
			}
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}
func (f *resourceRecoveryFixture) writeCaller(t *testing.T) (string, string) {
	t.Helper()
	err := f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		context := work.WorkContext{ID: "writer-context", ScopeID: "scope", RoleID: "leader", Workstream: "materials", Revision: 1}
		if e := tx.PutContext(context, 0); e != nil {
			return e
		}
		worker := work.WorkerSession{ID: "writer", ContextID: context.ID, ScopeID: "scope", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "writer-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if e := tx.PutWorker(worker, 0); e != nil {
			return e
		}
		input := work.Inbox{ID: "writer-input", ScopeID: "scope", RecipientRoleID: "leader", RecipientContextID: context.ID, EventID: "work", State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: f.now, Revision: 1}
		if e := tx.PutInbox(input, 0); e != nil {
			return e
		}
		attempt := work.ExecutionAttempt{ID: "writer-attempt", ContextID: context.ID, ScopeID: "scope", RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "writer-run"}, State: work.AttemptDispatchPending, InputIDs: []string{input.ID}, CommandID: "writer-start", Revision: 1}
		start := work.Operation{ID: attempt.CommandID, ScopeID: "scope", ContextID: context.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "fixture", State: work.OperationPrepared, Revision: 1}
		if e := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: start, Now: f.now}); e != nil {
			return e
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := f.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: "scope", ContextID: "writer-context", WorkerID: "writer", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return token, "writer-run"
}
func TestResourceRecoveryLiveCallDoesNotNotifyRestartCanRecoverAndLateReceiptCannotOverwriteChoice(t *testing.T) {
	f := newResourceRecoveryFixture(t)
	if err := f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		r, e := tx.GetResource("scope", "target")
		if e != nil {
			return e
		}
		r.SourceIdentity = "folder:parent"
		r.CanonicalURI = "https://a.feishu.cn/drive/folder/parent" // identities are immutable: use a separate actual registered parent.
		r.ID = "parent"
		return tx.PutResource(r, 0)
	}); err != nil {
		t.Fatal(err)
	}
	token, run := f.writeCaller(t)
	f.connector.entered = make(chan struct{})
	f.connector.release = make(chan struct{})
	originalService := f.a.teamRuntime.service
	req := workservice.CreateResourceDocumentRequest{Key: "held-create", ParentResourceID: "parent", ExpectedParentRevision: 1, Title: "New doc", Purpose: "Work", CollectionID: "materials"}
	type outcome struct {
		result workservice.ResourceWriteResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		r, e := originalService.ResourceCreateDocument(context.Background(), token, run, req)
		done <- outcome{r, e}
	}()
	select {
	case <-f.connector.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("write never reached fake SDK")
	}
	if err := f.a.runTeamResourceWriteRecoveryViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews("scope")
		if e == nil && len(views) != 0 {
			t.Fatal("live held SDK call prompted premature human recovery")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := workservice.NewWithResourceConnectors(f.a.teamRuntime.store, f.signer, func() time.Time { return f.now }, map[string]workservice.ResourceConnector{"leader": f.connector})
	if err != nil {
		t.Fatal(err)
	}
	f.a.teamRuntime.service = reopened
	if err = f.a.runTeamResourceWriteRecoveryViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	adopted, _ := f.inspectAndConfirm(t)
	if adopted.ObjectID == "" {
		t.Fatal("human chose no actual existing object")
	}
	close(f.connector.release)
	late := <-done
	if late.err != nil || late.result.Status != "original_result_recorded_after_recovery" {
		t.Fatalf("late result %+v %v", late.result, late.err)
	}
	replay, err := reopened.ResourceCreateDocument(t.Context(), token, run, req)
	if err != nil || replay.Status != "unknown_sealed_by_human" || f.connector.writes != 1 {
		t.Fatalf("sealed write replayed %+v %v writes%d", replay, err, f.connector.writes)
	}
	if err = f.a.runTeamResourceWriteRecoveryViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		original, e := tx.GetOperation(late.result.OperationID)
		if e != nil {
			return e
		}
		if original.State != work.OperationUnknown {
			t.Fatal("late result replaced original sealed state")
		}
		lateSeen := false
		for _, step := range original.Steps {
			if strings.HasPrefix(step.ID, "late_original_receipt:") {
				content, e := tx.GetContent("scope", step.Receipt)
				if e != nil {
					return e
				}
				lateSeen = strings.Contains(string(content.Data), "late-created")
			}
		}
		if !lateSeen {
			t.Fatal("real late SDK receipt lost")
		}
		resource, e := tx.GetResource("scope", adopted.ObjectID)
		if e != nil {
			return e
		}
		if resource.SourceIdentity != "docx:chosen" {
			t.Fatal("late created object silently replaced human choice")
		}
		views, e := tx.ListUIViews("scope")
		notice := false
		for _, view := range views {
			notice = notice || view.Kind == "resource.write.recovery.late"
		}
		if !notice {
			t.Fatal("late fact had no human notice")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	observed, err := reopened.ResourceObserveWrite(t.Context(), token, run, workservice.ObserveResourceWriteRequest{Key: "after-late", OperationID: late.result.OperationID})
	if err != nil || len(observed.LateReceiptRefs) != 1 {
		t.Fatalf("late facts not readable from existing observation entry %+v %v", observed, err)
	}
}

func TestResourceRecoveryFailedCallClearsActivityAndOffersActualHumanRecovery(t *testing.T) {
	f := newResourceRecoveryFixture(t)
	if err := f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		r := f.resource
		r.ID = "parent"
		r.SourceIdentity = "folder:parent"
		r.CanonicalURI = "https://a.feishu.cn/drive/folder/parent"
		return tx.PutResource(r, 0)
	}); err != nil {
		t.Fatal(err)
	}
	token, run := f.writeCaller(t)
	f.connector.entered = make(chan struct{})
	f.connector.release = make(chan struct{})
	close(f.connector.release)
	f.connector.createErr = errors.New("actual SDK response ended unexpectedly")
	result, err := f.a.teamRuntime.service.ResourceCreateDocument(t.Context(), token, run, workservice.CreateResourceDocumentRequest{Key: "failed-call", ParentResourceID: "parent", ExpectedParentRevision: 1, Title: "Requested doc", Purpose: "Work", CollectionID: "materials"})
	if err != nil || result.Status != "pending_external" {
		t.Fatalf("unknown call %+v %v", result, err)
	}
	if f.a.teamRuntime.service.ResourceWriteCallInFlight(result.OperationID) {
		t.Fatal("finished call retained live activity token")
	}
	if err = f.a.runTeamResourceWriteRecoveryViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews("scope")
		if e == nil && (len(views) != 1 || views[0].Kind != "resource.write.recovery") {
			t.Fatal("completed uncertain call has no actual recovery card")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}
func TestResourceRecoveryProofCannotBeBorrowedAcrossWriteOrScope(t *testing.T) {
	f := newResourceRecoveryFixture(t)
	_ = f.unknown(t, "resource.document.create")
	if err := f.a.runTeamResourceWriteRecoveryViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, _ := f.inspectAndConfirm(t)
	if err := f.a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		if e := tx.PutScope(work.WorkScope{ID: "foreign", Kind: work.ScopePersonal, PrincipalID: f.owner, RoleID: "leader", Revision: 1}, 0); e != nil {
			return e
		}
		for _, scope := range []string{"scope", "foreign"} {
			op := work.Operation{ID: "borrow-" + scope, ScopeID: scope, Actor: work.Actor{Kind: work.ActorRole, ID: "leader"}, Kind: "resource.document.create", TargetID: "target", TargetGeneration: 1, IdempotencyKey: "borrow-" + scope, RequestHash: "fake", State: work.OperationUnknown, Revision: 1, Steps: []work.OperationStep{{ID: "human_resource_recovery", State: work.OperationConfirmed, Receipt: result.OperationID}}}
			if _, _, e := tx.PutOperation(op, 0); e != nil {
				return e
			}
			valid, e := tx.ResourceWriteWasRecovered(op)
			if e == nil || valid {
				t.Fatal("another write/scope borrowed an unrelated actual human decision")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
