package workservice

import (
	"context"
	"fmt"
	"reflect"
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
	ReportingOnly        bool                      `json:"reporting_only,omitempty"`
}

type AgreementApplyResult struct {
	Result
	MigratedRequestIDs []string   `json:"migrated_request_ids"`
	DecisionOwner      work.Actor `json:"decision_owner"`
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
			change := workstore.ApplyAgreementRequest{Agreement: next, ExpectedRevision: req.ExpectedRevision, ReportingOnly: req.ReportingOnly}
			if req.ReportingOnly && req.ExpectedRevision == 0 {
				return "", 0, fmt.Errorf("reporting-only changes require an existing agreement")
			}
			authorityChanged := req.ExpectedRevision > 0 && (old.DecisionOwner != next.DecisionOwner || !reflect.DeepEqual(old.ParticipationPoints, next.ParticipationPoints))
			if authorityChanged && !req.ReportingOnly {
				requests, err := tx.ListDecisionRequests(c.Worker.ScopeID)
				if err != nil {
					return "", 0, err
				}
				for _, request := range requests {
					if request.AgreementID != id || request.State != work.DecisionOpen || request.Kind != work.DecisionBusiness {
						continue
					}
					replacement := request
					replacement.ID = stableID("decision-request", request.ID, strconv.FormatInt(next.Revision, 10))
					replacement.Owner = next.DecisionOwner
					replacement.AllowedActors = []work.Actor{next.DecisionOwner}
					replacement.AgreementRevision = next.Revision
					replacement.RequestGeneration++
					replacement.Revision = 1
					replacement.CreatedAt = s.now()
					replacement.SupersededBy = ""
					replacement.ResponseOperationID = ""
					change.Replacements = append(change.Replacements, workstore.DecisionReplacement{OldRequestID: request.ID, ExpectedRevision: request.Revision, Replacement: replacement})
					result.MigratedRequestIDs = append(result.MigratedRequestIDs, replacement.ID)
					if replacement.Owner.Kind == work.ActorRole {
						change.Inbox = append(change.Inbox, work.Inbox{ID: stableID("decision-inbox", replacement.ID), ScopeID: c.Worker.ScopeID, RecipientRoleID: replacement.Owner.ID, EventID: "decision:" + replacement.ID, State: work.InboxPending, AvailableAt: s.now(), Revision: 1})
					}
				}
			}
			if err := tx.ApplyAgreement(change); err != nil {
				return "", 0, err
			}
			result.DecisionOwner = next.DecisionOwner
			return id, next.Revision, nil
		})
		if err == nil {
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
			}
		}
		return err
	})
	return result, err
}
