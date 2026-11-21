package workservice

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type agreementConflictProposal struct {
	Agreement work.ParticipationAgreement `json:"agreement"`
	Request   AgreementApplyRequest       `json:"request"`
}

func (s *Service) findAgreementConflict(tx *workstore.Tx, next work.ParticipationAgreement) (work.ParticipationAgreement, error) {
	current, err := tx.ListAgreements(next.ScopeID)
	if err != nil {
		return work.ParticipationAgreement{}, err
	}
	for _, old := range current {
		if old.ID == next.ID || old.SubjectKind != next.SubjectKind || old.SubjectID != next.SubjectID || old.ValidFrom.After(s.now()) || (old.ValidUntil != nil && !old.ValidUntil.After(s.now())) {
			continue
		}
		return old, nil
	}
	return work.ParticipationAgreement{}, nil
}

func sameAgreementTerms(old, next work.ParticipationAgreement) bool {
	sameEnd := old.ValidUntil == nil && next.ValidUntil == nil ||
		old.ValidUntil != nil && next.ValidUntil != nil && old.ValidUntil.Equal(*next.ValidUntil)
	return old.DecisionOwner == next.DecisionOwner && old.Reporting == next.Reporting && old.Validity == next.Validity && sameEnd &&
		sameAgreementValues(old.ParticipationPoints, next.ParticipationPoints) && sameAgreementValues(old.AffectedPrincipalIDs, next.AffectedPrincipalIDs)
}

func sameAgreementValues(a, b []string) bool {
	left, right := slices.Clone(a), slices.Clone(b)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

func (s *Service) requestAgreementConflict(tx *workstore.Tx, c Caller, old, next work.ParticipationAgreement, req AgreementApplyRequest) (string, int64, error) {
	if err := next.Validate(); err != nil {
		return "", 0, err
	}
	if old.SubjectKind != next.SubjectKind || old.SubjectID != next.SubjectID || old.Revision != req.ExpectedRevision {
		return "", 0, workstore.ErrConflict
	}
	if err := validateAgreementImpactCoverage(tx, next, old, req.TaskImpacts); err != nil {
		return "", 0, err
	}
	if req.ReportingOnly {
		if err := work.ValidateReportingChange(old, next); err != nil {
			return "", 0, err
		}
	}
	// The current initiator retains control over their agreement. A conflicting
	// author cannot replace it by changing the decision owner in the proposal.
	id := stableID("agreement_conflict", old.ID, next.Source.ID, req.Key)
	proposal := agreementConflictProposal{Agreement: next, Request: req}
	data, err := json.Marshal(proposal)
	if err != nil {
		return "", 0, err
	}
	private := work.NewContent(stableID("agreement_proposal", id), old.ScopeID, "application/json", data, s.now())
	if err = tx.PutContent(private); err != nil {
		return "", 0, err
	}
	text := fmt.Sprintf("现行约定 %s（第 %d 版）由 %s 发起。%s 提出的修订会将参与点从 %v 改为 %v，将决定责任从 %s 改为 %s，将汇报从 %s 改为 %s。请选择保留或采纳。", old.ID, old.Revision, old.Initiator.ID, next.Initiator.ID, old.ParticipationPoints, next.ParticipationPoints, old.DecisionOwner.ID, next.DecisionOwner.ID, old.Reporting, next.Reporting)
	prompt := work.NewContent(stableID("agreement_conflict_prompt", id), old.ScopeID, "text/plain; charset=utf-8", []byte(text), s.now())
	if err = tx.PutContent(prompt); err != nil {
		return "", 0, err
	}
	request := work.DecisionRequest{
		ID: id, ScopeID: old.ScopeID, Kind: work.DecisionBusiness, State: work.DecisionOpen,
		Owner: old.Initiator, AllowedActors: []work.Actor{old.Initiator}, AgreementProposalRef: private.ID,
		AttemptID: c.Attempt.ID, RequestGeneration: 1, Revision: 1, CreatedAt: s.now(),
		Presentation: &work.DecisionPresentation{Title: "处理参与约定冲突", PromptRef: prompt.ID,
			Choices: []work.DecisionChoice{{Value: "keep_current", Label: "保留现行约定"}, {Value: "adopt_proposal", Label: "采纳新提议"}}},
	}
	if err = tx.PutDecisionRequest(request, 0); err != nil {
		return "", 0, err
	}
	return request.ID, request.Revision, nil
}

func (s *Service) applyAgreementConflictDecision(tx *workstore.Tx, request work.DecisionRequest, decision work.Decision) error {
	if decision.Choice != "adopt_proposal" {
		return nil
	}
	body, err := tx.GetContent(request.ScopeID, request.AgreementProposalRef)
	if err != nil {
		return err
	}
	var proposal agreementConflictProposal
	if err = json.Unmarshal(body.Data, &proposal); err != nil {
		return err
	}
	next := proposal.Agreement
	old, err := tx.GetAgreement(request.ScopeID, next.ID)
	if err != nil {
		return err
	}
	if old.Initiator != decision.Actor || old.Revision != proposal.Request.ExpectedRevision || next.ScopeID != old.ScopeID {
		return workstore.ErrConflict
	}
	source, err := tx.GetMessage(proposal.Request.SourceMessageID)
	if err != nil {
		return err
	}
	if source.RecalledEvidenceRef != "" || humanSource(source) != next.Source {
		return workstore.ErrConflict
	}
	next.Initiator = decision.Actor
	next.Source = decision.Source
	change, _, err := s.agreementChange(tx, next, old, proposal.Request, request.AttemptID)
	if err != nil {
		return err
	}
	return tx.ApplyAgreement(change)
}
