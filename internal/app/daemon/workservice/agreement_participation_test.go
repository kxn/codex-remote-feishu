package workservice

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func agreementTestActor(t *testing.T, s *Service, messageID string) work.Actor {
	t.Helper()
	var actor work.Actor
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		message, err := tx.GetMessage(messageID)
		actor = message.Source
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return actor
}

func respondAgreementTestCard(t *testing.T, s *Service, request work.DecisionRequest, sender, choice string) error {
	t.Helper()
	viewID := "view-" + request.ID + "-" + sender
	var interactionID string
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		binding, err := tx.GetRoleBotBinding("leader")
		if err != nil {
			return err
		}
		gateway := work.IngressGateway{RoleID: binding.RoleID, GatewayRef: binding.GatewayRef, AppID: binding.AppID, BotGeneration: binding.IdentityGeneration}
		projection := work.NewContent("body-"+viewID, request.ScopeID, "application/json", []byte(`{"card":"decision"}`), s.now())
		if err = tx.PutContent(projection); err != nil {
			return err
		}
		op := work.Operation{ID: "publish-" + viewID, ScopeID: request.ScopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "ui"}, Kind: "feishu.publish", TargetID: "group", TargetGeneration: 1, IdempotencyKey: viewID, RequestHash: projection.SHA256, RequestRef: projection.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		raw := work.HumanInteractionEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "click-" + viewID, Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: sender}, ChatID: "group", MessageID: "card-" + viewID, Kind: "card", Values: map[string]any{"team_view": viewID, "action": "decision.choose", "choice": choice}, ReceivedAt: s.now()}
		interaction, _, err := tx.AcceptHumanInteraction(raw, gateway)
		if err != nil {
			return err
		}
		interactionID = interaction.ID
		return tx.PutUIView(workstore.UIView{ID: viewID, ScopeID: request.ScopeID, SurfaceKey: "business.decision:" + request.ID, Kind: "business.decision", TargetID: request.ID, TargetRevision: request.Revision, Binding: binding, Tenant: "tenant", ChatID: "group", PrincipalID: request.Owner.ID, ChannelGeneration: 1, BodyRef: projection.ID, OperationID: op.ID, ExternalMessageID: raw.MessageID, Revision: 1, CreatedAt: s.now()}, 0)
	}); err != nil {
		return err
	}
	_, err := s.RespondHumanDecision(t.Context(), interactionID, viewID, choice)
	return err
}

func TestAgreementCrossPrincipalConflictPreservesDecisionUntilExistingInitiatorActs(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false, "first-member", "second-member")
	first := agreementTestActor(t, s, "first-member")
	req := AgreementApplyRequest{Key: "initial-agreement", ID: "attention", SourceMessageID: "first-member", SubjectKind: work.AgreementProject, SubjectID: "project", DecisionOwner: first, ParticipationPoints: []string{"review"}, Reporting: work.ReportingAll, Validity: work.AgreementPersistent}
	if _, err := s.AgreementApply(t.Context(), token, run, req); err != nil {
		t.Fatal(err)
	}
	initial, err := s.RequestDecision(t.Context(), token, run, RequestDecisionRequest{Key: "open-review", Title: "Review", Prompt: "Which do you want?", Choices: []work.DecisionChoice{{Value: "keep", Label: "Keep"}, {Value: "replace", Label: "Replace"}}, AgreementID: req.ID})
	if err != nil {
		t.Fatal(err)
	}
	alternate := req
	alternate.ID, alternate.Key, alternate.SourceMessageID = "other-agreement", "different-key", "second-member"
	alternate.DecisionOwner = work.Actor{Kind: work.ActorRole, ID: "leader"}
	alternate.ParticipationPoints = nil
	coordination, err := s.AgreementApply(t.Context(), token, run, alternate)
	if err != nil || coordination.Status != "needs_coordination" || coordination.DecisionOwner != first {
		t.Fatalf("different ID bypassed the prior participation terms: %+v %v", coordination, err)
	}
	req.ExpectedRevision = 1
	req.Key = "conflicting-agreement"
	req.SourceMessageID = "second-member"
	req.DecisionOwner = work.Actor{Kind: work.ActorRole, ID: "leader"}
	changed, err := s.AgreementApply(t.Context(), token, run, req)
	if err != nil || changed.Status != "needs_coordination" || changed.DecisionOwner != first {
		t.Fatalf("conflict receipt %+v %v", changed, err)
	}
	var conflict work.DecisionRequest
	if err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		var err error
		conflict, err = tx.GetDecisionRequest("project-scope", changed.ObjectID)
		if err != nil {
			return err
		}
		request, err := tx.GetDecisionRequest("project-scope", initial.ObjectID)
		if err == nil && (request.State != work.DecisionOpen || request.Owner != first) {
			t.Fatalf("proposal changed the incumbent decision %+v", request)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DecisionRespond(t.Context(), token, run, DecisionRespondRequest{Key: "model-cannot-adopt", RequestID: conflict.ID, ExpectedRevision: 1, RequestGeneration: 1, Choice: "adopt_proposal"}); err == nil {
		t.Fatal("role applied the conflict without the first member")
	}
	if err = respondAgreementTestCard(t, s, conflict, "second-member", "adopt_proposal"); err == nil {
		t.Fatal("proposer approved their own conflicting change")
	}
	if err = respondAgreementTestCard(t, s, conflict, "first-member", "adopt_proposal"); err != nil {
		t.Fatal(err)
	}
	if err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		old, err := tx.GetDecisionRequest("project-scope", initial.ObjectID)
		if err != nil {
			return err
		}
		migrated, err := tx.GetDecisionRequest("project-scope", old.SupersededBy)
		if err == nil && (old.State != work.DecisionSuperseded || migrated.Owner != req.DecisionOwner) {
			t.Fatalf("confirmed proposal did not migrate decision %+v %+v", old, migrated)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAgreementAddsOnlySelectedBeforePointHoldAndReopensAfterDecision(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false, "participant")
	person := agreementTestActor(t, s, "participant")
	var tasks []work.Task
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		for _, id := range []string{"before-task", "past-task", "unselected-task"} {
			task := work.Task{ID: id, ScopeID: "project-scope", Title: id, OwnerRoleID: "leader", State: work.TaskTodo, RequirementsRevision: 1, AssignmentGeneration: 1, Revision: 1}
			if err := tx.PutTask(task, 0); err != nil {
				return err
			}
			tasks = append(tasks, task)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	req := AgreementApplyRequest{Key: "new-point", ID: "attention", SourceMessageID: "participant", SubjectKind: work.AgreementPhase, SubjectID: "design", DecisionOwner: person, ParticipationPoints: []string{"Review the design"}, Reporting: work.ReportingAll, Validity: work.AgreementPersistent,
		TaskImpacts: []AgreementTaskImpact{
			{TaskID: tasks[0].ID, TaskRevision: 1, Point: "Review the design", Stage: agreementStageBefore, Facts: "Implementation has not started. Proceed with the frozen prototype?", Choices: []work.DecisionChoice{{Value: "approve", Label: "Proceed"}, {Value: "revise", Label: "Revise"}}},
			{TaskID: tasks[1].ID, TaskRevision: 1, Point: "Review the design", Stage: agreementStagePassed, Facts: "The current design was implemented earlier. Remaining changes can still be reviewed."},
			{TaskID: tasks[2].ID, TaskRevision: 1, Point: "Review the design", Stage: agreementNotApplicable, Facts: "This task updates billing documentation and makes no design changes."},
		},
	}
	for _, impacts := range [][]AgreementTaskImpact{nil, req.TaskImpacts[:2]} {
		incomplete := req
		incomplete.TaskImpacts = impacts
		if _, err := s.AgreementApply(t.Context(), token, run, incomplete); err == nil {
			t.Fatal("an unfinished task without an explicit impact or exclusion did not block the agreement")
		}
	}
	stale := req
	stale.TaskImpacts = append([]AgreementTaskImpact{}, req.TaskImpacts...)
	stale.TaskImpacts[1].TaskRevision++
	if _, err := s.AgreementApply(t.Context(), token, run, stale); err == nil {
		t.Fatal("a stale selected task did not roll back the agreement")
	}
	result, err := s.AgreementApply(t.Context(), token, run, req)
	if err != nil || len(result.TaskOutcomes) != 3 || result.TaskOutcomes[0].DecisionRequestID == "" || result.TaskOutcomes[1].DecisionRequestID != "" || result.TaskOutcomes[2].DecisionRequestID != "" {
		t.Fatalf("participation outcomes %+v %v", result, err)
	}
	var request work.DecisionRequest
	check := func(blocked bool) error {
		return s.store.Within(t.Context(), func(tx *workstore.Tx) error {
			var err error
			request, err = tx.GetDecisionRequest("project-scope", result.TaskOutcomes[0].DecisionRequestID)
			if err != nil {
				return err
			}
			gates, err := tx.ListPauseGates("project-scope")
			if err != nil {
				return err
			}
			for i, task := range tasks {
				allow, err := work.GatesAllowRun(task.ScopeID, task.ID, gates)
				if err != nil {
					return err
				}
				if allow == (i == 0 && blocked) {
					t.Fatalf("unexpected admission for %s, allow=%v", task.ID, allow)
				}
			}
			return nil
		})
	}
	if err = check(true); err != nil {
		t.Fatal(err)
	}
	if err = respondAgreementTestCard(t, s, request, "participant", "approve"); err != nil {
		t.Fatal(err)
	}
	if err = check(false); err != nil {
		t.Fatal(err)
	}
	if request.State != work.DecisionResolved {
		t.Fatalf("expected recorded human conclusion: %+v", request)
	}
}
