package workservice

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func TestAgreementActualMigrationAndRoleCannotImpersonateHuman(t *testing.T) {
	s, token, run := knowledgeService(t)
	ctx := context.Background()
	req := AgreementApplyRequest{Key: "agreement", ID: "agreement", SourceMessageID: "human-input", SubjectKind: work.AgreementProject, SubjectID: "scope", DecisionOwner: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, ParticipationPoints: []string{"review"}, Reporting: work.ReportingAll, Validity: work.AgreementPersistent}
	created, err := s.AgreementApply(ctx, token, run, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.PutDecisionRequest(work.DecisionRequest{ID: "pending-human", ScopeID: "scope", Kind: work.DecisionBusiness, State: work.DecisionOpen, Owner: req.DecisionOwner, AllowedActors: []work.Actor{req.DecisionOwner}, AgreementID: created.ObjectID, AgreementRevision: 1, RequestGeneration: 1, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	response := DecisionRespondRequest{Key: "cannot-approve-as-human", RequestID: "pending-human", ExpectedRevision: 1, RequestGeneration: 1, AgreementRevision: 1, Choice: "approve"}
	if _, err := s.DecisionRespond(ctx, token, run, response); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("Agent acted as human: %v", err)
	}
	spoof := workcmd.DispatchRequest{Name: "decision.respond", RunRef: run, Payload: json.RawMessage(`{"key":"spoof","request_id":"pending-human","expected_revision":1,"request_generation":1,"agreement_revision":1,"choice":"approve","actor":{"kind":"principal","id":"human"}}`)}
	if _, handled, err := s.executeExtended(ctx, token, spoof); !handled || err == nil {
		t.Fatalf("actor DTO accepted: handled=%v err=%v", handled, err)
	}
	req.Key = "delegate"
	req.ExpectedRevision = 1
	req.DecisionOwner = work.Actor{Kind: work.ActorRole, ID: "leader"}
	migrated, err := s.AgreementApply(ctx, token, run, req)
	if err != nil || len(migrated.MigratedRequestIDs) != 1 {
		t.Fatalf("migration %#v, %v", migrated, err)
	}
	replay, err := s.AgreementApply(ctx, token, run, req)
	if err != nil || !replay.Replay || len(replay.MigratedRequestIDs) != 1 || replay.DecisionOwner != req.DecisionOwner {
		t.Fatalf("migration replay %#v, %v", replay, err)
	}
	response.Key = "stale-human-card"
	if _, err := s.DecisionRespond(ctx, token, run, response); !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("old decision revision accepted: %v", err)
	}
	response = DecisionRespondRequest{Key: "delegated-role-choice", RequestID: migrated.MigratedRequestIDs[0], ExpectedRevision: 1, RequestGeneration: 2, AgreementRevision: 2, Choice: "approve"}
	accepted, err := s.DecisionRespond(ctx, token, run, response)
	if err != nil || accepted.Status != "accepted" {
		t.Fatalf("role response %#v, %v", accepted, err)
	}
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		old, err := tx.GetDecisionRequest("scope", "pending-human")
		if err != nil {
			return err
		}
		if old.State != work.DecisionSuperseded {
			t.Error("old pending owner remained active")
		}
		current, err := tx.GetDecisionRequest("scope", response.RequestID)
		if err != nil {
			return err
		}
		if current.State != work.DecisionRecorded {
			t.Error("response falsely claimed effect resolved")
		}
		choices, err := tx.ListDecisions("scope", current.ID)
		if err != nil {
			return err
		}
		if len(choices) != 1 || choices[0].Actor != (work.Actor{Kind: work.ActorRole, ID: "leader"}) {
			t.Error("stored response actor was not derived from Caller")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
