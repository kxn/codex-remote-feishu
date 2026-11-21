package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"strconv"
	"time"
)

func (t *Tx) PutDecisionRequest(v work.DecisionRequest, expected int64) error {
	scope, err := t.GetScope(v.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeLibrary || scope.Kind == work.ScopeBootstrap && v.Kind != work.DecisionNativeRequest {
		return ErrScopeDenied
	}
	if err := v.Validate(); err != nil {
		return err
	}
	if err = t.validateDecisionPresentation(v, expected == 0); err != nil {
		return err
	}
	if v.TaskID != "" {
		task, err := t.GetTask(v.TaskID)
		if err != nil {
			return err
		}
		if task.ScopeID != v.ScopeID {
			return ErrScopeDenied
		}
	}
	if v.AttemptID != "" {
		attempt, err := t.GetAttempt(v.AttemptID)
		if err != nil {
			return err
		}
		if attempt.ScopeID != v.ScopeID {
			return ErrScopeDenied
		}
	}
	if v.AgreementID != "" {
		if expected == 0 {
			if _, err := t.RequireActiveAgreement(v.ScopeID, v.AgreementID, v.AgreementRevision, time.Now().UTC()); err != nil {
				return err
			}
		}
		agreement, err := t.GetAgreementVersion(v.ScopeID, v.AgreementID, v.AgreementRevision)
		if err != nil {
			return err
		}
		if v.Owner != agreement.DecisionOwner {
			return fmt.Errorf("request owner differs from adopted agreement")
		}
	}
	for _, actor := range append([]work.Actor{v.Owner}, v.AllowedActors...) {
		if actor.Kind == work.ActorRole {
			if err := t.requireRoleInScope(v.ScopeID, actor.ID); err != nil {
				return err
			}
		} else if actor.Kind == work.ActorPrincipal {
			if err := t.exists("principals", actor.ID); err != nil {
				return err
			}
		}
	}
	if expected == 0 && v.State != work.DecisionOpen {
		return fmt.Errorf("new decision request must be open")
	}
	if expected > 0 {
		old, err := t.GetDecisionRequest(v.ScopeID, v.ID)
		if err != nil {
			return err
		}
		if err = immutable(old.ScopeID != v.ScopeID || old.TaskID != v.TaskID || old.Kind != v.Kind || old.AttemptID != v.AttemptID || old.RequestGeneration != v.RequestGeneration || old.AgreementID != v.AgreementID || old.AgreementRevision != v.AgreementRevision || !reflect.DeepEqual(old.Presentation, v.Presentation) || old.Owner != v.Owner || !reflect.DeepEqual(old.AllowedActors, v.AllowedActors)); err != nil {
			return err
		}
		if err = work.ValidateDecisionTransition(old.State, v.State); err != nil {
			return err
		}
		if old.State == work.DecisionOpen && v.State == work.DecisionRecorded {
			var count int
			if err = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM decisions WHERE request_id=? AND request_revision=?", v.ID, expected).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("recorded request requires accepted decision")
			}
		}
	}
	if v.State == work.DecisionSuperseded {
		replacement, err := t.GetDecisionRequest(v.ScopeID, v.SupersededBy)
		if err != nil {
			return err
		}
		if replacement.Kind != v.Kind || replacement.TaskID != v.TaskID {
			return fmt.Errorf("request replacement binding mismatch")
		}
	}
	if v.ResponseOperationID != "" {
		operation, err := t.GetOperation(v.ResponseOperationID)
		if err != nil {
			return err
		}
		if operation.ScopeID != v.ScopeID || operation.TargetID != v.ID || operation.TargetGeneration != v.RequestGeneration {
			return fmt.Errorf("decision response operation binding mismatch")
		}
		if v.State == work.DecisionResolved && operation.State != work.OperationConfirmed {
			return fmt.Errorf("decision effect not confirmed")
		}
	} else if v.State == work.DecisionApplying || v.State == work.DecisionResolved {
		return fmt.Errorf("applying request requires response operation")
	}
	return t.record("decision_requests", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetDecisionRequest(scopeID, id string) (work.DecisionRequest, error) {
	var v work.DecisionRequest
	err := t.scopedRead("decision_requests", scopeID, id, &v)
	return v, err
}
func (t *Tx) ListDecisionRequests(scopeID string) ([]work.DecisionRequest, error) {
	return listRecords[work.DecisionRequest](t, "decision_requests", " WHERE scope_id=?", scopeID)
}
func (t *Tx) GetDecision(scopeID, id string) (work.Decision, error) {
	var v work.Decision
	if err := t.read("decisions", id, &v); err != nil {
		return v, err
	}
	if _, err := t.GetDecisionRequest(scopeID, v.RequestID); err != nil {
		return work.Decision{}, err
	}
	return v, nil
}
func (t *Tx) ListDecisions(scopeID, requestID string) ([]work.Decision, error) {
	if _, err := t.GetDecisionRequest(scopeID, requestID); err != nil {
		return nil, err
	}
	return listRecords[work.Decision](t, "decisions", " WHERE request_id=?", requestID)
}

type AcceptDecisionRequest struct {
	ScopeID           string
	Decision          work.Decision
	ResponseOperation work.Operation
}

func (t *Tx) AcceptDecision(input AcceptDecisionRequest) error {
	return t.atomic(func() error {
		decision := input.Decision
		if existing, e := t.GetDecision(input.ScopeID, decision.ID); e == nil {
			if reflect.DeepEqual(existing, decision) {
				operation, err := t.GetOperation(input.ResponseOperation.ID)
				if err != nil {
					return err
				}
				if err = work.ValidateReplay(operation, input.ResponseOperation); err != nil {
					return ErrIdempotencyConflict
				}
				return nil
			}
			return ErrIdempotencyConflict
		} else if e != ErrNotFound {
			return e
		}
		request, err := t.GetDecisionRequest(input.ScopeID, decision.RequestID)
		if err != nil {
			return err
		}
		if err = t.validateDecisionPresentation(request, true); err != nil {
			return err
		}
		if err = work.ValidateDecision(request, decision); err != nil {
			return err
		}
		if err = t.validateInstructionSource(input.ScopeID, decision.Actor, decision.Source); err != nil {
			return err
		}
		operation := input.ResponseOperation
		if operation.ScopeID != request.ScopeID || operation.TargetID != request.ID || operation.TargetGeneration != request.RequestGeneration || operation.State != work.OperationPrepared {
			return fmt.Errorf("decision operation mismatch")
		}
		canonical, created, err := t.PutOperation(operation, 0)
		if err != nil {
			return err
		}
		if !created || canonical.ID != operation.ID {
			return ErrIdempotencyConflict
		}
		if _, err = t.tx.ExecContext(t.ctx, "INSERT INTO decisions(id,payload) VALUES(?,?)", decision.ID, mustJSON(decision)); err != nil {
			return err
		}
		expected := request.Revision
		request.Revision++
		request.State = work.DecisionRecorded
		request.ResponseOperationID = operation.ID
		return t.PutDecisionRequest(request, expected)
	})
}
func (t *Tx) validateInstructionSource(scopeID string, actor work.Actor, source work.SourceRef) error {
	if err := source.Validate(); err != nil {
		return err
	}

	switch source.Kind {
	case "human_interaction":
		if actor.Kind != work.ActorPrincipal {
			return ErrScopeDenied
		}
		interaction, err := t.GetHumanInteraction(source.ID)
		if err != nil {
			return err
		}
		if interaction.PrincipalID != actor.ID || source.Version != strconv.FormatInt(interaction.Revision, 10) {
			return ErrScopeDenied
		}
		viewID, _ := interaction.Envelope.Values["team_view"].(string)
		view, err := t.GetUIView(viewID)
		if err != nil {
			return err
		}
		if view.ScopeID != scopeID {
			return ErrScopeDenied
		}
		if err = t.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		return t.requireContent(scopeID, source.EvidenceRef)

	case "message":
		message, err := t.GetMessage(source.ID)
		if err != nil {
			return err
		}
		if message.RecalledEvidenceRef != "" || message.ScopeID != scopeID || message.Source != actor || source.Version != strconv.FormatInt(message.Revision, 10) {
			return fmt.Errorf("instruction actor/source mismatch")
		}
		return nil
	case "attempt":
		if actor.Kind != work.ActorRole {
			return fmt.Errorf("native execution cannot impersonate a human instruction")
		}
		attempt, err := t.GetAttempt(source.ID)
		if err != nil {
			return err
		}
		worker, err := t.GetWorker(attempt.RunRef.WorkerID)
		if err != nil {
			return err
		}
		if attempt.ScopeID != scopeID || worker.RoleID != actor.ID || source.Version != strconv.FormatInt(attempt.RunRef.Generation, 10) {
			return fmt.Errorf("instruction attempt actor mismatch")
		}
		return nil
	default:
		return fmt.Errorf("instruction requires a recorded message or role attempt source")
	}
}
