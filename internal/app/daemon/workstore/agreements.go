package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
)

func (t *Tx) PutAgreement(v work.ParticipationAgreement, expected int64) error {
	return t.atomic(func() error {
		if expected > 0 {
			old, err := t.GetAgreement(v.ScopeID, v.ID)
			if err != nil {
				return err
			}
			if old.DecisionOwner != v.DecisionOwner || !reflect.DeepEqual(old.ParticipationPoints, v.ParticipationPoints) {
				requests, e := t.ListDecisionRequests(v.ScopeID)
				if e != nil {
					return e
				}
				for _, request := range requests {
					if request.AgreementID == v.ID && request.State == work.DecisionOpen {
						return fmt.Errorf("use ApplyAgreement to migrate affected pending decisions")
					}
				}
			}
		}
		return t.putAgreement(v, expected)
	})
}
func (t *Tx) putAgreement(v work.ParticipationAgreement, expected int64) error {
	scope, err := t.GetScope(v.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeBootstrap {
		return ErrScopeDenied
	}
	return t.atomic(func() error {
		if err := v.Validate(); err != nil {
			return err
		}
		if err := t.validateInstructionSource(v.ScopeID, v.Initiator, v.Source); err != nil {
			return err
		}
		if v.DecisionOwner.Kind == work.ActorRole {
			if err := t.requireRoleInScope(v.ScopeID, v.DecisionOwner.ID); err != nil {
				return err
			}
		} else if v.DecisionOwner.Kind == work.ActorPrincipal {
			if err := t.exists("principals", v.DecisionOwner.ID); err != nil {
				return err
			}
		}
		if expected > 0 {
			old, err := t.GetAgreement(v.ScopeID, v.ID)
			if err != nil {
				return err
			}
			if old.ScopeID != v.ScopeID || old.SubjectKind != v.SubjectKind || old.SubjectID != v.SubjectID {
				return fmt.Errorf("agreement scope and subject are immutable")
			}
		}
		if err := t.record("agreements", v.ID, v.Revision, expected, v); err != nil {
			return err
		}
		_, err := t.tx.ExecContext(t.ctx, "INSERT INTO agreement_versions(agreement_id,revision,payload) VALUES(?,?,?)", v.ID, v.Revision, mustJSON(v))
		return err
	})
}
func (t *Tx) GetAgreement(scopeID, id string) (work.ParticipationAgreement, error) {
	var v work.ParticipationAgreement
	err := t.scopedRead("agreements", scopeID, id, &v)
	return v, err
}
func (t *Tx) GetAgreementVersion(scopeID, id string, revision int64) (work.ParticipationAgreement, error) {
	var v work.ParticipationAgreement
	var payload string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM agreement_versions WHERE agreement_id=? AND revision=?", id, revision).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal([]byte(payload), &v); err != nil {
		return v, err
	}
	if v.ScopeID != scopeID {
		return work.ParticipationAgreement{}, ErrScopeDenied
	}
	return v, nil
}
func (t *Tx) ListAgreements(scopeID string) ([]work.ParticipationAgreement, error) {
	return listRecords[work.ParticipationAgreement](t, "agreements", " WHERE scope_id=?", scopeID)
}

type DecisionReplacement struct {
	OldRequestID     string
	ExpectedRevision int64
	Replacement      work.DecisionRequest
}
type ApplyAgreementRequest struct {
	Agreement        work.ParticipationAgreement
	ExpectedRevision int64
	ReportingOnly    bool
	Replacements     []DecisionReplacement
	Withdrawals      []work.DecisionRequest
	NewRequests      []work.DecisionRequest
	Inbox            []work.Inbox
	Gates            []work.PauseGate
}

func (t *Tx) ApplyAgreement(input ApplyAgreementRequest) error {
	return t.atomic(func() error {
		agreement := input.Agreement
		var old work.ParticipationAgreement
		var err error
		if input.ExpectedRevision > 0 {
			old, err = t.GetAgreement(agreement.ScopeID, agreement.ID)
			if err != nil {
				return err
			}
			if input.ReportingOnly {
				if err = work.ValidateReportingChange(old, agreement); err != nil {
					return err
				}
				if !reflect.DeepEqual(old.ParticipationPoints, agreement.ParticipationPoints) || !reflect.DeepEqual(old.AffectedPrincipalIDs, agreement.AffectedPrincipalIDs) {
					return fmt.Errorf("reporting change alters participation")
				}
				if len(input.Replacements) > 0 || len(input.Withdrawals) > 0 || len(input.NewRequests) > 0 || len(input.Gates) > 0 {
					return fmt.Errorf("reporting change cannot migrate decisions")
				}
			}
		}
		if err = t.putAgreement(agreement, input.ExpectedRevision); err != nil {
			return err
		}
		for _, gate := range input.Gates {
			if gate.ScopeID != agreement.ScopeID || gate.Source != work.GateParticipation || gate.State != work.GateClosed {
				return fmt.Errorf("invalid agreement participation gate")
			}
			if err = t.PutPauseGate(gate, 0); err != nil {
				return err
			}
		}
		migrated := map[string]bool{}
		for _, move := range input.Replacements {
			request, e := t.GetDecisionRequest(agreement.ScopeID, move.OldRequestID)
			if e != nil {
				return e
			}
			if request.Revision != move.ExpectedRevision {
				return ErrConflict
			}
			if request.Kind != work.DecisionBusiness || request.State != work.DecisionOpen || request.AgreementID != agreement.ID {
				return fmt.Errorf("only matching open business decisions can be migrated")
			}
			replacement := move.Replacement
			if replacement.ID == request.ID || replacement.ScopeID != request.ScopeID || replacement.TaskID != request.TaskID || replacement.Kind != request.Kind || replacement.State != work.DecisionOpen || replacement.Owner != agreement.DecisionOwner || replacement.AgreementID != agreement.ID || replacement.AgreementRevision != agreement.Revision || replacement.RequestGeneration != request.RequestGeneration+1 {
				return fmt.Errorf("decision migration binding mismatch")
			}
			if err = t.PutDecisionRequest(replacement, 0); err != nil {
				return err
			}
			request.State = work.DecisionSuperseded
			request.SupersededBy = replacement.ID
			request.Revision++
			if err = t.PutDecisionRequest(request, move.ExpectedRevision); err != nil {
				return err
			}
			migrated[request.ID] = true
			if replacement.Owner.Kind == work.ActorRole {
				found := false
				for _, queued := range input.Inbox {
					if queued.EventID == "decision:"+replacement.ID && queued.RecipientRoleID == replacement.Owner.ID {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("delegated decision needs receiver inbox")
				}
			}
		}
		for _, withdrawal := range input.Withdrawals {
			request, e := t.GetDecisionRequest(agreement.ScopeID, withdrawal.ID)
			if e != nil {
				return e
			}
			if request.Revision != withdrawal.Revision || request.State != work.DecisionOpen || request.AgreementID != agreement.ID || request.ParticipationGateID == "" {
				return ErrConflict
			}
			request.State = work.DecisionAborted
			request.Revision++
			if err = t.PutDecisionRequest(request, withdrawal.Revision); err != nil {
				return err
			}
		}
		if input.ExpectedRevision > 0 && !input.ReportingOnly && (old.DecisionOwner != agreement.DecisionOwner || !reflect.DeepEqual(old.ParticipationPoints, agreement.ParticipationPoints)) {
			requests, e := t.ListDecisionRequests(agreement.ScopeID)
			if e != nil {
				return e
			}
			for _, request := range requests {
				if request.Kind == work.DecisionBusiness && request.State == work.DecisionOpen && request.AgreementID == agreement.ID && request.AgreementRevision != agreement.Revision && !migrated[request.ID] {
					return fmt.Errorf("agreement leaves affected pending decision unmigrated")
				}
			}
		}
		for _, request := range input.NewRequests {
			if request.ScopeID != agreement.ScopeID || request.AgreementID != agreement.ID || request.AgreementRevision != agreement.Revision {
				return fmt.Errorf("new participation request outside agreement")
			}
			if err = t.PutDecisionRequest(request, 0); err != nil {
				return err
			}
		}
		for _, queued := range input.Inbox {
			if queued.ScopeID != agreement.ScopeID || queued.State != work.InboxPending {
				return fmt.Errorf("agreement inbox outside scope")
			}
			if err = t.PutInbox(queued, 0); err != nil {
				return err
			}
		}
		return nil
	})
}
