package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type RequestDecisionRequest struct {
	Key             string                `json:"key"`
	Title           string                `json:"title"`
	Prompt          string                `json:"prompt"`
	TaskID          string                `json:"task_id,omitempty"`
	AgreementID     string                `json:"agreement_id,omitempty"`
	SourceMessageID string                `json:"source_message_id,omitempty"`
	Choices         []work.DecisionChoice `json:"choices"`
	References      []work.DeliveryRef    `json:"references,omitempty"`
	SubmissionID    string                `json:"submission_id,omitempty"`
}

// RequestDecision freezes the actual object/version/options. Owner is derived
// from an adopted agreement or this run's real human instruction; otherwise it
// is the project's responsible role. DTOs cannot invent a human decision owner.
func (s *Service) RequestDecision(ctx context.Context, token, run string, req RequestDecisionRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		id := stableID("decision-request", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		var err error
		result, err = s.mutate(tx, c, "decision.request", req.Key, id, req, func() (string, int64, error) {
			encodedQuestion, _ := json.Marshal(struct {
				Title, Prompt string
				Choices       []work.DecisionChoice
			}{req.Title, req.Prompt, req.Choices})
			if strings.TrimSpace(req.Prompt) == "" || len(encodedQuestion) > 20*1024 {
				return "", 0, fmt.Errorf("decision requires a concrete bounded question")
			}
			scope, err := tx.GetScope(c.Worker.ScopeID)
			if err != nil {
				return "", 0, err
			}
			owner := work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}
			if scope.Kind == work.ScopeProject {
				project, err := tx.GetProject(scope.ProjectID)
				if err != nil {
					return "", 0, err
				}
				owner.ID = project.LeaderRoleID
			}
			var agreementRevision int64
			if req.AgreementID != "" {
				agreement, err := tx.GetAgreement(scope.ID, req.AgreementID)
				if err != nil {
					return "", 0, err
				}
				if agreement.ValidUntil != nil && !s.now().Before(*agreement.ValidUntil) {
					return "", 0, fmt.Errorf("participation agreement expired")
				}
				if agreement.SubjectKind == work.AgreementItem && agreement.SubjectID != req.TaskID {
					return "", 0, fmt.Errorf("participation agreement applies to another item")
				}
				owner = agreement.DecisionOwner
				agreementRevision = agreement.Revision
			} else if req.SourceMessageID != "" {
				message, _, err := currentHumanInstruction(tx, c, req.SourceMessageID)
				if err != nil {
					return "", 0, err
				}
				owner = message.Source
			}
			prompt := work.NewContent(stableID("decision-prompt", id), scope.ID, "text/plain; charset=utf-8", []byte(req.Prompt), s.now())
			if err = tx.PutContent(prompt); err != nil {
				return "", 0, err
			}
			presentation := &work.DecisionPresentation{Title: req.Title, PromptRef: prompt.ID, Choices: append([]work.DecisionChoice(nil), req.Choices...), References: append([]work.DeliveryRef(nil), req.References...), SubmissionID: req.SubmissionID}
			if req.TaskID != "" {
				task, err := tx.GetTask(req.TaskID)
				if err != nil {
					return "", 0, err
				}
				if task.ScopeID != scope.ID {
					return "", 0, workstore.ErrScopeDenied
				}
				if err = mayManageTask(tx, c, task); err != nil {
					return "", 0, err
				}
				presentation.TaskRequirementsRevision = task.RequirementsRevision
				presentation.TaskAssignmentGeneration = task.AssignmentGeneration
				if req.SubmissionID != "" {
					submitted, err := readReviewSubmission(tx, task, req.SubmissionID)
					if err != nil {
						return "", 0, err
					}
					presentation.RequirementID = submitted.RequirementID
					if len(req.References) == 0 {
						return "", 0, fmt.Errorf("review requires the explicit saved artifact versions shown to the reviewer")
					}
					for _, requirement := range task.Requirements {
						if requirement.ID == submitted.RequirementID {
							if requiredActor := requirement.EvidenceActors[submitted.Kind]; requiredActor != "" && requiredActor != owner.Kind {
								return "", 0, fmt.Errorf("this evidence requires a %s decision owner from the applicable agreement or real instruction", requiredActor)
							}
						}
					}
					offered := false
					for _, choice := range req.Choices {
						offered = offered || choice.Value == "approve"
					}
					if !offered {
						return "", 0, fmt.Errorf("evidence review requires explicit approve choice")
					}
				}
			} else if req.SubmissionID != "" {
				return "", 0, fmt.Errorf("submission review requires its explicit task")
			}
			for _, ref := range req.References {
				resource, err := tx.GetResource(scope.ID, ref.ResourceID)
				if err != nil {
					return "", 0, err
				}
				version, err := tx.GetResourceVersion(scope.ID, ref.VersionID)
				if err != nil {
					return "", 0, err
				}
				if version.ResourceID != resource.ID || !version.Saved || !version.Complete {
					return "", 0, fmt.Errorf("decision requires saved complete artifact versions")
				}
			}
			request := work.DecisionRequest{ID: id, ScopeID: scope.ID, TaskID: req.TaskID, Kind: work.DecisionBusiness, State: work.DecisionOpen, Owner: owner, AllowedActors: []work.Actor{owner}, AttemptID: c.Attempt.ID, AgreementID: req.AgreementID, AgreementRevision: agreementRevision, RequestGeneration: 1, Presentation: presentation, Revision: 1, CreatedAt: s.now()}
			if err = tx.PutDecisionRequest(request, 0); err != nil {
				return "", 0, err
			}
			return id, 1, nil
		})
		return err
	})
	return result, err
}
func readReviewSubmission(tx *workstore.Tx, task work.Task, id string) (TaskEvidenceSubmitRequest, error) {
	var original TaskEvidenceSubmitRequest
	submission, err := tx.GetOperation(id)
	if err != nil {
		return original, err
	}
	if submission.ScopeID != task.ScopeID || submission.TargetID != task.ID || submission.Kind != "task.evidence.submit" || submission.State != work.OperationConfirmed {
		return original, workauth.ErrUnauthorized
	}
	body, err := tx.GetContent(task.ScopeID, submission.RequestRef)
	if err != nil {
		return original, err
	}
	if err = json.Unmarshal(body.Data, &original); err != nil {
		return original, err
	}
	original.ExpectedRevision = task.Revision
	return original, validateCurrentSubmission(task, original)
}

// RespondHumanDecision is trusted UI application code, never an agent command.
// The real persisted SDK interaction and current view identify the principal.
func (s *Service) RespondHumanDecision(ctx context.Context, interactionID, viewID, choice string) (Result, error) {
	var result Result
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, err := tx.GetHumanInteraction(interactionID)
		if err != nil {
			return err
		}
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		actualView, _ := interaction.Envelope.Values["team_view"].(string)
		actualChoice, _ := interaction.Envelope.Values["choice"].(string)
		actualAction, _ := interaction.Envelope.Values["action"].(string)
		if actualView != viewID || actualChoice != choice || actualAction != "decision.choose" {
			return workauth.ErrUnauthorized
		}
		if view.Kind != "business.decision" {
			return workauth.ErrUnauthorized
		}
		if err = tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		scope, err := tx.GetScope(view.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeProject {
			if err = tx.RequireProjectPrincipalAccess(view.ScopeID, interaction.PrincipalID, s.now()); err != nil {
				return err
			}
		}
		request, err := tx.GetDecisionRequest(view.ScopeID, view.TargetID)
		if err != nil {
			return err
		}
		replayID := stableID("human-decision-effect", interaction.ID)
		if old, e := tx.GetOperation(replayID); e == nil {
			if old.ScopeID != view.ScopeID || old.TargetID != request.ID || old.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}) || old.RequestHash != stableID("decision-hash", request.ID, choice, strconv.FormatInt(request.RequestGeneration, 10)) {
				return workstore.ErrIdempotencyConflict
			}
			result = Result{Status: "recorded", OperationID: old.ID, ObjectID: request.ID, Revision: request.Revision, Replay: true}
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		if request.Revision != view.TargetRevision || request.Kind != work.DecisionBusiness || request.State != work.DecisionOpen || request.Presentation == nil {
			return workstore.ErrConflict
		}
		actor := work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}
		allowed := false
		for _, permitted := range request.AllowedActors {
			allowed = allowed || permitted == actor
		}
		if !allowed {
			return workauth.ErrUnauthorized
		}
		proof, err := json.Marshal(interaction)
		if err != nil {
			return err
		}
		evidence := work.NewContent(stableID("human-proof", interaction.ID), view.ScopeID, "application/json", proof, s.now())
		if err = tx.PutContent(evidence); err != nil {
			return err
		}
		body := work.NewContent(stableID("human-decision-body", interaction.ID), view.ScopeID, "text/plain", []byte("用户对“"+request.Presentation.Title+"”选择："+choice), s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: stableID("human-decision-source", interaction.ID), ScopeID: view.ScopeID, ConversationID: stableID("decision-conversation", request.ID), Source: actor, BodyRef: body.ID, DerivedFrom: &work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: strconv.FormatInt(interaction.Revision, 10), EvidenceRef: evidence.ID}, Revision: 1, CreatedAt: s.now()}
		if _, _, err = tx.AcceptMessage(message, nil); err != nil {
			return err
		}
		source := work.SourceRef{Kind: "message", ID: message.ID, Version: "1", EvidenceRef: body.ID}
		decision := work.Decision{ID: stableID("human-decision", interaction.ID), RequestID: request.ID, RequestRevision: request.Revision, RequestGeneration: request.RequestGeneration, AgreementRevision: request.AgreementRevision, Actor: actor, Choice: choice, Source: source, CreatedAt: s.now()}
		op := work.Operation{ID: stableID("human-decision-effect", interaction.ID), ScopeID: view.ScopeID, Actor: actor, Kind: "decision.apply", TargetID: request.ID, TargetGeneration: request.RequestGeneration, IdempotencyKey: interaction.ID, RequestHash: stableID("decision-hash", request.ID, choice, strconv.FormatInt(request.RequestGeneration, 10)), State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if err = tx.AcceptDecision(workstore.AcceptDecisionRequest{ScopeID: view.ScopeID, Decision: decision, ResponseOperation: op}); err != nil {
			return err
		}
		if err = s.applyBusinessDecision(tx, request, decision, op, message.ID); err != nil {
			return err
		}
		result = Result{Status: "recorded", OperationID: op.ID, ObjectID: request.ID, Revision: request.Revision + 3}
		return nil
	})
	return result, err
}
