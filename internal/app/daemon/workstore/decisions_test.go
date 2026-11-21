package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func instruction(t *testing.T, s *Store, id string) work.SourceRef {
	t.Helper()
	requireTx(t, s, func(tx *Tx) error {
		_, _, err := tx.AcceptMessage(work.Message{ID: id, ScopeID: "s", ConversationID: "chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, BodyRef: "body", Revision: 1}, nil)
		return err
	})
	return work.SourceRef{Kind: "message", ID: id, Version: "1", EvidenceRef: "body"}
}
func agreement(source work.SourceRef) work.ParticipationAgreement {
	return work.ParticipationAgreement{ID: "agreement", ScopeID: "s", Initiator: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, Source: source, SubjectKind: work.AgreementProject, SubjectID: "project", DecisionOwner: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, Reporting: work.ReportingAll, Validity: work.AgreementPersistent, Revision: 1}
}
func decisionRequest() work.DecisionRequest {
	return work.DecisionRequest{ID: "request", ScopeID: "s", Kind: work.DecisionBusiness, State: work.DecisionOpen, Owner: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, AllowedActors: []work.Actor{{Kind: work.ActorPrincipal, ID: "p"}}, AgreementID: "agreement", AgreementRevision: 1, RequestGeneration: 1, Revision: 1}
}
func seedDecision(t *testing.T, s *Store) work.SourceRef {
	t.Helper()
	source := instruction(t, s, "human-instruction")
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutAgreement(agreement(source), 0); err != nil {
			return err
		}
		return tx.PutDecisionRequest(decisionRequest(), 0)
	})
	return source
}
func TestDecisionAcceptanceSeparatesAuthorityAndAppliedEffect(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	source := seedDecision(t, s)
	decision := work.Decision{ID: "accepted", RequestID: "request", RequestRevision: 1, RequestGeneration: 1, AgreementRevision: 1, Actor: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, Choice: "approve", Source: source}
	operation := testOperation("response")
	operation.Kind = "apply_decision"
	operation.TargetID = "request"
	requireTx(t, s, func(tx *Tx) error {
		spoof := decision
		spoof.Source = work.SourceRef{Kind: "attempt", ID: "anything", Version: "1", EvidenceRef: "body"}
		if err := tx.AcceptDecision(AcceptDecisionRequest{ScopeID: "s", Decision: spoof, ResponseOperation: operation}); err == nil {
			t.Fatal("native attempt impersonated human approval")
		}
		request, err := tx.GetDecisionRequest("s", "request")
		if err != nil {
			return err
		}
		if request.State != work.DecisionOpen {
			t.Fatal("rejected approval changed request")
		}
		return tx.AcceptDecision(AcceptDecisionRequest{ScopeID: "s", Decision: decision, ResponseOperation: operation})
	})
	requireTx(t, s, func(tx *Tx) error {
		request, err := tx.GetDecisionRequest("s", "request")
		if err != nil {
			return err
		}
		if request.State != work.DecisionRecorded {
			t.Fatal("acceptance inferred applied effect")
		}
		request.State = work.DecisionApplying
		request.Revision++
		if err = tx.PutDecisionRequest(request, 2); err != nil {
			return err
		}
		request.State = work.DecisionResolved
		request.Revision++
		if err = tx.PutDecisionRequest(request, 3); err == nil {
			t.Fatal("unconfirmed remote effect resolved request")
		}
		operation, err := tx.GetOperation("response")
		if err != nil {
			return err
		}
		operation.State = work.OperationSending
		operation.Revision++
		if _, _, err = tx.PutOperation(operation, 1); err != nil {
			return err
		}
		operation.State = work.OperationConfirmed
		operation.ExternalReceipt = "native-accepted"
		operation.Revision++
		if _, _, err = tx.PutOperation(operation, 2); err != nil {
			return err
		}
		return tx.PutDecisionRequest(request, 3)
	})
}
func TestAgreementMigrationSupersedesWithoutApproving(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedDecision(t, s)
	source := instruction(t, s, "delegate-instruction")
	next := agreement(source)
	next.Revision = 2
	next.DecisionOwner = work.Actor{Kind: work.ActorRole, ID: "r"}
	replacement := decisionRequest()
	replacement.ID = "replacement"
	replacement.Owner = next.DecisionOwner
	replacement.AllowedActors = []work.Actor{next.DecisionOwner}
	replacement.AgreementRevision = 2
	replacement.RequestGeneration = 2
	change := ApplyAgreementRequest{Agreement: next, ExpectedRevision: 1, Replacements: []DecisionReplacement{{OldRequestID: "request", ExpectedRevision: 1, Replacement: replacement}}}
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.ApplyAgreement(change); err == nil {
			t.Fatal("delegation without recipient inbox accepted")
		}
		current, err := tx.GetAgreement("s", "agreement")
		if err != nil {
			return err
		}
		if current.Revision != 1 {
			t.Fatal("failed migration committed agreement")
		}
		if _, err = tx.GetDecisionRequest("s", "replacement"); !errors.Is(err, ErrNotFound) {
			t.Fatal("failed migration leaked replacement")
		}
		return nil
	})
	queued := testInput("decision-inbox", "r", "c-r")
	queued.EventID = "decision:replacement"
	change.Inbox = []work.Inbox{queued}
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.ApplyAgreement(change); err != nil {
			return err
		}
		old, err := tx.GetDecisionRequest("s", "request")
		if err != nil {
			return err
		}
		if old.State != work.DecisionSuperseded || old.SupersededBy != "replacement" {
			t.Fatal("old card remains active")
		}
		current, err := tx.GetDecisionRequest("s", "replacement")
		if err != nil {
			return err
		}
		if current.State != work.DecisionOpen || current.Owner != next.DecisionOwner {
			t.Fatal("delegation inferred accepted decision")
		}
		decisions, err := tx.ListDecisions("s", "replacement")
		if err != nil {
			return err
		}
		if len(decisions) != 0 {
			t.Fatal("delegation fabricated approval")
		}
		oldDecision := work.Decision{ID: "late", RequestID: "request", RequestRevision: 1, RequestGeneration: 1, AgreementRevision: 1, Actor: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, Choice: "approve", Source: source}
		operation := testOperation("late-response")
		operation.TargetID = "request"
		if err = tx.AcceptDecision(AcceptDecisionRequest{ScopeID: "s", Decision: oldDecision, ResponseOperation: operation}); err == nil {
			t.Fatal("old card decision accepted")
		}
		return nil
	})
}
func TestReportingOnlyCannotTransferDecisionOwnership(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	source := seedDecision(t, s)
	next := agreement(source)
	next.Revision = 2
	next.Reporting = work.ReportingActionRequired
	requireTx(t, s, func(tx *Tx) error {
		bad := next
		bad.DecisionOwner = work.Actor{Kind: work.ActorRole, ID: "r"}
		if err := tx.ApplyAgreement(ApplyAgreementRequest{Agreement: bad, ExpectedRevision: 1, ReportingOnly: true}); err == nil {
			t.Fatal("reporting silently delegated decision")
		}
		if err := tx.ApplyAgreement(ApplyAgreementRequest{Agreement: next, ExpectedRevision: 1, ReportingOnly: true}); err != nil {
			return err
		}
		request, err := tx.GetDecisionRequest("s", "request")
		if err != nil {
			return err
		}
		if request.Owner != next.DecisionOwner || request.State != work.DecisionOpen {
			t.Fatal("reporting changed responsibility")
		}
		return nil
	})
}
