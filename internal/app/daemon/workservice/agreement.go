package workservice

import (
	"context"
	"strconv"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type AgreementApplyRequest struct {
	Key                  string                    `json:"key"`
	ID                   string                    `json:"id,omitempty"`
	ExpectedRevision     int64                     `json:"expected_revision"`
	SourceMessageID      string                    `json:"source_message_id"`
	SubjectKind          work.AgreementSubjectKind `json:"subject_kind"`
	SubjectID            string                    `json:"subject_id"`
	DecisionOwner        work.Actor                `json:"decision_owner"`
	ParticipationPoints  []string                  `json:"participation_points"`
	AffectedPrincipalIDs []string                  `json:"affected_principal_ids"`
	Reporting            work.ReportingGranularity `json:"reporting"`
	Validity             work.AgreementValidity    `json:"validity"`
	ValidUntil           *time.Time                `json:"valid_until,omitempty"`
	TaskImpacts          []AgreementTaskImpact     `json:"task_impacts,omitempty"`
	ReportingOnly        bool                      `json:"reporting_only,omitempty"`
}

type AgreementApplyResult struct {
	Result
	MigratedRequestIDs []string               `json:"migrated_request_ids"`
	TaskOutcomes       []AgreementTaskOutcome `json:"task_outcomes,omitempty"`
	DecisionOwner      work.Actor             `json:"decision_owner"`
	CoordinationState  work.DecisionState     `json:"coordination_state,omitempty"`
}

func (s *Service) AgreementList(ctx context.Context, token, runRef string, req KnowledgeListRequest) (KnowledgePage[work.ParticipationAgreement], error) {
	var result KnowledgePage[work.ParticipationAgreement]
	err := s.withManagementWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		items, err := tx.ListAgreements(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		result, err = knowledgePage(items, req, func(v work.ParticipationAgreement) string { return v.ID })

		return err
	})
	return result, err
}

func (s *Service) AgreementApply(ctx context.Context, token, runRef string, req AgreementApplyRequest) (AgreementApplyResult, error) {
	var result AgreementApplyResult
	result.MigratedRequestIDs = []string{}
	err := s.withManagementWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		id := req.ID
		if id == "" {
			id = stableID("agreement", c.Worker.ScopeID, req.Key)
		}
		var err error
		result.Result, err = s.mutate(tx, c, "agreement.apply", req.Key, id, req, func() (string, int64, error) {
			message, _, err := currentHumanInstruction(tx, c, req.SourceMessageID)
			if err != nil {
				return "", 0, err
			}
			scope, err := tx.GetScope(c.Worker.ScopeID)
			if err != nil {
				return "", 0, err
			}
			if req.SubjectKind == work.AgreementProject {
				wanted := scope.ProjectID
				if scope.Kind == work.ScopePersonal {
					wanted = scope.ID
				}
				if req.SubjectID != wanted {
					return "", 0, workauth.ErrUnauthorized
				}
			}
			if req.SubjectKind == work.AgreementItem {
				task, err := tx.GetTask(req.SubjectID)
				if err != nil {
					return "", 0, err
				}
				if err = mayManageTask(tx, c, task); err != nil {
					return "", 0, err
				}
			}
			if scope.Kind == work.ScopePersonal && req.DecisionOwner.Kind == work.ActorPrincipal && req.DecisionOwner.ID != scope.PrincipalID {
				return "", 0, workauth.ErrUnauthorized
			}
			if scope.Kind == work.ScopeProject && (req.SubjectKind != work.AgreementItem || req.DecisionOwner.Kind == work.ActorPrincipal) {
				project, e := tx.GetProject(scope.ProjectID)
				if e != nil {
					return "", 0, e
				}
				if project.LeaderRoleID != c.Worker.RoleID {
					return "", 0, workauth.ErrUnauthorized
				}
			}
			source := work.SourceRef{Kind: "message", ID: message.ID, Version: strconv.FormatInt(message.Revision, 10), EvidenceRef: message.BodyRef}
			next := work.ParticipationAgreement{ID: id, ScopeID: c.Worker.ScopeID, Initiator: message.Source, AffectedPrincipalIDs: req.AffectedPrincipalIDs, Source: source, SubjectKind: req.SubjectKind, SubjectID: req.SubjectID, DecisionOwner: req.DecisionOwner, ParticipationPoints: req.ParticipationPoints, Reporting: req.Reporting, Validity: req.Validity, Revision: req.ExpectedRevision + 1, ValidFrom: s.now(), ValidUntil: req.ValidUntil}
			var old work.ParticipationAgreement
			if req.ExpectedRevision > 0 {
				old, err = tx.GetAgreement(c.Worker.ScopeID, id)
				if err != nil {
					return "", 0, err
				}
				next.ValidFrom = old.ValidFrom
			}
			if err = validateAgreementTaskImpacts(tx, c, next, req.TaskImpacts); err != nil {
				return "", 0, err
			}
			if req.ExpectedRevision > 0 && old.Initiator != message.Source {
				if old.Revision == req.ExpectedRevision && old.SubjectKind == next.SubjectKind && old.SubjectID == next.SubjectID && len(req.TaskImpacts) == 0 && sameAgreementTerms(old, next) {
					return old.ID, old.Revision, nil
				}
				return s.requestAgreementConflict(tx, c, old, next, req)
			}
			if scope.Kind == work.ScopeProject {
				existing, e := s.findAgreementConflict(tx, next)
				if e != nil {
					return "", 0, e
				}
				if existing.ID != "" {
					if sameAgreementTerms(existing, next) && len(req.TaskImpacts) == 0 {
						return existing.ID, existing.Revision, nil
					}
					// A different key cannot manufacture a new independent authority.
					// Explicit updates to legacy duplicate records must reconcile first.
					if req.ExpectedRevision > 0 {
						return "", 0, workstore.ErrConflict
					}
					req.ID, req.ExpectedRevision = existing.ID, existing.Revision
					next.ID, next.Revision, next.ValidFrom = existing.ID, existing.Revision+1, existing.ValidFrom
					old = existing
					if existing.Initiator != message.Source {
						return s.requestAgreementConflict(tx, c, existing, next, req)
					}
				}
			}
			change, outcomes, e := s.agreementChange(tx, next, old, req, c.Attempt.ID)
			if e != nil {
				return "", 0, e
			}
			if e = tx.ApplyAgreement(change); e != nil {
				return "", 0, e
			}
			result.TaskOutcomes = outcomes
			for _, replacement := range change.Replacements {
				result.MigratedRequestIDs = append(result.MigratedRequestIDs, replacement.Replacement.ID)
			}
			result.DecisionOwner = next.DecisionOwner
			return next.ID, next.Revision, nil
		})
		if err == nil {
			if result.ObjectID != id {
				if agreement, e := tx.GetAgreement(c.Worker.ScopeID, result.ObjectID); e == nil {
					id = agreement.ID
					result.DecisionOwner = agreement.DecisionOwner
				}
			}
			if result.ObjectID != id {
				result.Status = "needs_coordination"
				request, e := tx.GetDecisionRequest(c.Worker.ScopeID, result.ObjectID)
				if e != nil {
					return e
				}
				result.DecisionOwner = request.Owner
				result.CoordinationState = request.State
				if request.State == work.DecisionResolved {
					result.Status = "coordination_resolved"
				}
				return nil
			}
			result.DecisionOwner = req.DecisionOwner
			if result.Replay {
				requests, e := tx.ListDecisionRequests(c.Worker.ScopeID)
				if e != nil {
					return e
				}
				for _, request := range requests {
					if request.AgreementID == id && request.AgreementRevision == result.Revision && request.RequestGeneration > 1 {
						result.MigratedRequestIDs = append(result.MigratedRequestIDs, request.ID)
					}
				}
				for _, impact := range req.TaskImpacts {
					result.TaskOutcomes = append(result.TaskOutcomes, impact.outcome(id, result.Revision))
				}
			}
		}
		return err
	})
	return result, err
}
