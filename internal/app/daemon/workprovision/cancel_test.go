package workprovision

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func cancellationHuman(t *testing.T, f *provisionFixture) string {
	t.Helper()
	id := "actual-cancellation-human"
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		body := work.NewContent("cancel-human-body", f.scopeID, "text/plain", []byte("Cancel the project establishment; keep actual resources and reconcile any unknown creation."), f.now.Add(time.Minute))
		if err := tx.PutContent(body); err != nil {
			return err
		}
		_, _, err := tx.AcceptMessage(work.Message{ID: id, ScopeID: f.scopeID, ConversationID: "project-chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "project-chat", MessageID: "cancel-platform-message"}, BodyRef: body.ID, Revision: 1, CreatedAt: f.now.Add(time.Minute)}, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestCancellationRetainsUnknownCreateUntilExactHumanRecovery(t *testing.T) {
	f := newFixture(t)
	f.leader.createErr = errors.New("create response lost")
	if _, err := f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
	source := cancellationHuman(t, f)
	baselineAppend, baselineTabs, baselinePublish := f.leader.appendCalls, f.leader.tabCalls, f.leader.publishCalls
	var revision int64
	if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(f.operationID)
		revision = op.Revision
		return err
	}); err != nil {
		t.Fatal(err)
	}
	request := CancelProvisioningRequest{OperationID: f.operationID, ExpectedRevision: revision, SourceMessageID: source}
	if _, err := f.service.CancelProvisioningWithHumanProof(context.Background(), "impostor", request); err == nil {
		t.Fatal("caller-declared human canceled another principal's operation")
	}
	report, err := f.service.CancelProvisioningWithHumanProof(context.Background(), "human", request)
	if err != nil || report.State != string(work.OperationUnknown) || len(report.Blockers) == 0 {
		t.Fatalf("unknown create erased by cancellation: %#v %v", report, err)
	}
	var step workstore.ProvisionStep
	if err = f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		var err error
		step, err = tx.GetProvisionStep(f.operationID, "materials.document")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.service.ResolveUnknown(context.Background(), Recovery{OperationID: f.operationID, StepID: step.StepID, ExpectedRevision: step.Revision, ExternalID: f.leader.doc.ID, SourceMessageID: source})
	if err != nil || recovered.State != string(work.OperationTerminal) {
		t.Fatalf("cancellation recovery %#v %v", recovered, err)
	}
	if f.leader.documentCalls != 1 || f.leader.appendCalls != baselineAppend || f.leader.tabCalls != baselineTabs || f.leader.publishCalls != baselinePublish {
		t.Fatal("canceled saga started remaining effects")
	}
	if _, err = f.service.Run(context.Background(), f.operationID); err != nil {
		t.Fatal(err)
	}
}
func TestCancellationDuringCreateKeepsKnownReceiptAndFencesFollowingWrites(t *testing.T) {
	f := newFixture(t)
	source := cancellationHuman(t, f)
	f.leader.afterCreate = func() {
		var revision int64
		if err := f.store.Within(context.Background(), func(tx *workstore.Tx) error {
			op, err := tx.GetOperation(f.operationID)
			revision = op.Revision
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.CancelProvisioningWithHumanProof(context.Background(), "human", CancelProvisioningRequest{OperationID: f.operationID, ExpectedRevision: revision, SourceMessageID: source}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.Run(context.Background(), f.operationID); !errors.Is(err, ErrCancelled) {
		t.Fatalf("in-flight run ignored cancellation fence: %v", err)
	}
	if f.leader.documentCalls != 1 || f.leader.appendCalls != 0 || f.leader.tabCalls != 0 {
		t.Fatal("later effects escaped cancellation")
	}
	report, err := f.service.Run(context.Background(), f.operationID)
	if err != nil || report.State != string(work.OperationTerminal) {
		t.Fatalf("settled cancellation %#v %v", report, err)
	}
	if err = f.store.Within(context.Background(), func(tx *workstore.Tx) error {
		step, err := tx.GetProvisionStep(f.operationID, "materials.document")
		if err != nil {
			return err
		}
		home, err := tx.GetProjectHome(f.projectID)
		if err != nil {
			return err
		}
		if step.ExternalID != "document-created" || home.DocumentID != step.ExternalID {
			t.Fatal("late actual document receipt was discarded")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
