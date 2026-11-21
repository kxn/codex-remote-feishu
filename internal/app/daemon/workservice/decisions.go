package workservice

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type DecisionRespondRequest struct {
	Key               string `json:"key"`
	RequestID         string `json:"request_id"`
	ExpectedRevision  int64  `json:"expected_revision"`
	RequestGeneration int64  `json:"request_generation"`
	AgreementRevision int64  `json:"agreement_revision"`
	Choice            string `json:"choice"`
}

func (s *Service) DecisionList(ctx context.Context, token, runRef string, req KnowledgeListRequest) (KnowledgePage[work.DecisionRequest], error) {
	var result KnowledgePage[work.DecisionRequest]
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		all, err := tx.ListDecisionRequests(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		items := []work.DecisionRequest{}
		for _, request := range all {
			if request.State == work.DecisionOpen || request.State == work.DecisionRecorded || request.State == work.DecisionApplying || request.State == work.DecisionUnknown {
				items = append(items, request)
			}
		}
		result, err = knowledgePage(items, req, func(v work.DecisionRequest) string { return v.ID })
		return err
	})
	return result, err
}

// DecisionRespond derives the role actor from the verified caller. There is no
// DTO field that can turn an Agent into a principal or approve on a human's behalf.
// Native/business effects remain pending in their own durable response operation.
func (s *Service) DecisionRespond(ctx context.Context, token, runRef string, req DecisionRespondRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		var err error
		result, err = s.mutate(tx, c, "decision.respond", req.Key, req.RequestID, req, func() (string, int64, error) {
			request, err := tx.GetDecisionRequest(c.Worker.ScopeID, req.RequestID)
			if err != nil {
				return "", 0, err
			}
			if request.Kind != work.DecisionBusiness {
				return "", 0, workauth.ErrUnauthorized
			}
			if request.State != work.DecisionOpen || request.Revision != req.ExpectedRevision || request.RequestGeneration != req.RequestGeneration || request.AgreementRevision != req.AgreementRevision {
				return "", 0, workstore.ErrConflict
			}
			allowed := false
			for _, actor := range request.AllowedActors {
				if actor == (work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}) {
					allowed = true
					break
				}
			}
			if !allowed {
				return "", 0, workauth.ErrUnauthorized
			}
			decision := work.Decision{ID: stableID("decision-response", c.Worker.ScopeID, c.Worker.RoleID, req.Key), RequestID: request.ID, RequestRevision: req.ExpectedRevision, RequestGeneration: req.RequestGeneration, AgreementRevision: req.AgreementRevision, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Choice: req.Choice, Source: callerSource(c), CreatedAt: s.now()}
			operation := work.Operation{ID: stableID("decision-effect", decision.ID), ScopeID: c.Worker.ScopeID, ContextID: c.Worker.ContextID, Actor: decision.Actor, Kind: "decision.apply", TargetID: request.ID, TargetGeneration: request.RequestGeneration, IdempotencyKey: req.Key, RequestHash: stableID("decision-hash", decision.ID, req.Choice), State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
			if err := tx.AcceptDecision(workstore.AcceptDecisionRequest{ScopeID: c.Worker.ScopeID, Decision: decision, ResponseOperation: operation}); err != nil {
				return "", 0, err
			}
			if request.Kind == work.DecisionBusiness && request.Presentation != nil {
				if err := s.applyBusinessDecision(tx, request, decision, operation, ""); err != nil {
					return "", 0, err
				}
			}
			return request.ID, request.Revision + 1, nil
		})
		return err
	})
	if err == nil {
		result.Status = "accepted"
	}
	return result, err
}
