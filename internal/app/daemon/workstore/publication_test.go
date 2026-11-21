package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func seedRunningAttempt(t *testing.T, s *Store) ClaimRequest {
	t.Helper()
	requireTx(t, s, func(tx *Tx) error { return tx.PutInbox(testInput("input", "r", "c-r"), 0) })
	claim := testClaim()
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(claim) })
	markRunning(t, s)
	return claim
}
func TestBodyReferencesMustResolveToDurableContent(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		message := work.Message{ID: "message", ScopeID: "s", ConversationID: "chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, BodyRef: "正文不应硬塞进reference", Revision: 1}
		if _, _, err := tx.AcceptMessage(message, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("non-durable body accepted: %v", err)
		}
		message.BodyRef = "body"
		_, _, err := tx.AcceptMessage(message, nil)
		return err
	})
	claim := seedRunningAttempt(t, s)
	outcome := testOutcome(claim)
	outcome.OutcomeRef = "not-saved"
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.CommitOutcome(outcome); !errors.Is(err, ErrNotFound) {
			t.Fatalf("non-durable outcome accepted: %v", err)
		}
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		if input.State != work.InboxClaimed {
			t.Fatal("failed outcome changed claim")
		}
		return nil
	})
}
func TestSavedDeliveryAndNotificationHaveSeparateFacts(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedRunningAttempt(t, s)
	requireTx(t, s, func(tx *Tx) error {
		resource := work.Resource{ID: "resource", ScopeID: "s", CollectionID: "results", ConnectorIdentity: "bot-app", SourceIdentity: "doc-token", CanonicalURI: "https://docs.example/document", Purpose: "result", Revision: 1}
		if err := tx.PutResource(resource, 0); err != nil {
			return err
		}
		body, err := tx.GetContent("s", "body")
		if err != nil {
			return err
		}
		version := work.ResourceVersion{ID: "version", ResourceID: "resource", ScopeID: "s", Kind: work.VersionImmutableSnapshot, Source: evidenceSource(), ContentRef: "body", ContentHash: body.SHA256, Consistency: work.ConsistencyAtomic, Complete: true, Saved: true}
		if err = tx.PutResourceVersion(version); err != nil {
			return err
		}
		version.ContentHash = "different"
		if err = tx.PutResourceVersion(version); err == nil {
			t.Fatal("saved version changed")
		}
		return tx.AddResourceCollection("s", "resource", "shared-index")
	})
	operation := testOperation("delivery-op")
	operation.Kind = "notify"
	operation.TargetID = "chat"
	outbox := work.Outbox{ID: "notification", ScopeID: "s", OperationID: operation.ID, Target: "chat", EventID: "delivery:delivery", PayloadRef: "body", State: work.OutboxPending, Revision: 1}
	delivery := work.Delivery{ID: "delivery", ScopeID: "s", Audience: []work.Actor{{Kind: work.ActorPrincipal, ID: "p"}}, Purpose: "result", Summary: "file saved", References: []work.DeliveryRef{{ResourceID: "resource", VersionID: "version"}}, Retention: work.RetentionPermanent, ConversationTarget: "chat", SourceAttemptID: "attempt", NotificationOutboxID: outbox.ID, Revision: 1}
	requireTx(t, s, func(tx *Tx) error {
		return tx.PublishDelivery(PublishDeliveryRequest{Delivery: delivery, Operation: operation, Outbox: outbox})
	})
	requireTx(t, s, func(tx *Tx) error {
		saved, err := tx.GetDelivery("s", "delivery")
		if err != nil {
			return err
		}
		notification, err := tx.GetOutbox(saved.NotificationOutboxID)
		if err != nil {
			return err
		}
		if notification.State != work.OutboxPending {
			t.Fatal("saved delivery inferred sent notification")
		}
		notification.State = work.OutboxSent
		notification.ExternalMessageID = "platform-receipt"
		notification.Revision++
		if err = tx.PutOutbox(notification, 1); err == nil {
			t.Fatal("notification skipped sending lifecycle")
		}
		return nil
	})
}
func TestReconcileQuiescencePinsCarrierAndRetainsUnknownOccupancy(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	claim := seedRunningAttempt(t, s)
	outcome := testOutcome(claim)
	outcome.Quiescent = false
	requireTx(t, s, func(tx *Tx) error { return tx.CommitOutcome(outcome) })
	proof := work.QuiescenceEvidence{RunRef: claim.Attempt.RunRef, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, NativeStopped: true, ContinuationStopped: true, BackgroundWorkStopped: true, Source: work.SourceRef{Kind: "runtime_quiescence", ID: "run", Version: "1", EvidenceRef: "stop-proof"}, ObservedAt: time.Now().UTC()}
	requireTx(t, s, func(tx *Tx) error {
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerRecovering {
			t.Fatal("uncertain outcome released worker")
		}
		if err = tx.ReconcileQuiescence(ReconcileQuiescenceRequest{Evidence: proof, ExpectedWorkerRevision: worker.Revision}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("proof without durable evidence accepted: %v", err)
		}
		return tx.PutContent(work.NewContent("stop-proof", "s", "application/json", []byte(`{"native_stopped":true,"continuation_stopped":true,"background_work_stopped":true}`), proof.ObservedAt))
	})
	requireTx(t, s, func(tx *Tx) error {
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		wrong := proof
		wrong.ProcessGeneration++
		if err = tx.ReconcileQuiescence(ReconcileQuiescenceRequest{Evidence: wrong, ExpectedWorkerRevision: worker.Revision}); err == nil {
			t.Fatal("foreign process proof released worker")
		}
		incomplete := proof
		incomplete.BackgroundWorkStopped = false
		if err = tx.ReconcileQuiescence(ReconcileQuiescenceRequest{Evidence: incomplete, ExpectedWorkerRevision: worker.Revision}); err == nil {
			t.Fatal("native stop alone released background work")
		}
		if err = tx.ReconcileQuiescence(ReconcileQuiescenceRequest{Evidence: proof, ExpectedWorkerRevision: worker.Revision}); err != nil {
			return err
		}
		if err = tx.ReconcileQuiescence(ReconcileQuiescenceRequest{Evidence: proof, ExpectedWorkerRevision: worker.Revision}); err != nil {
			return err
		}
		worker, err = tx.GetWorker("w")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerReady {
			t.Fatal("confirmed quiescent worker not ready")
		}
		var count int
		if err = tx.tx.QueryRowContext(tx.ctx, "SELECT count(*) FROM carrier_occupancy").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatal("carrier occupancy leaked")
		}
		return nil
	})
}
