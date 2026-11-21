package work

import (
	"fmt"
	"time"
)

type DecisionState string

const (
	DecisionOpen       DecisionState = "open"
	DecisionRecorded   DecisionState = "decision_recorded"
	DecisionApplying   DecisionState = "applying"
	DecisionResolved   DecisionState = "resolved"
	DecisionSuperseded DecisionState = "superseded"
	DecisionAborted    DecisionState = "aborted"
	DecisionUnknown    DecisionState = "unknown"
)

type DecisionKind string

const (
	DecisionBusiness      DecisionKind = "business"
	DecisionNativeRequest DecisionKind = "native_request"
)

type DecisionRequest struct {
	Presentation        *DecisionPresentation `json:"presentation,omitempty"`
	ID                  string                `json:"id"`
	ScopeID             string                `json:"scope_id"`
	TaskID              string                `json:"task_id,omitempty"`
	Kind                DecisionKind          `json:"kind"`
	State               DecisionState         `json:"state"`
	Owner               Actor                 `json:"owner"`
	AllowedActors       []Actor               `json:"allowed_actors"`
	AgreementID         string                `json:"agreement_id,omitempty"`
	AgreementRevision   int64                 `json:"agreement_revision"`
	RequestGeneration   int64                 `json:"request_generation"`
	AttemptID           string                `json:"attempt_id,omitempty"`
	SupersededBy        string                `json:"superseded_by,omitempty"`
	ResponseOperationID string                `json:"response_operation_id,omitempty"`
	Revision            int64                 `json:"revision"`
	CreatedAt           time.Time             `json:"created_at"`
}

func (v DecisionRequest) Validate() error {
	if v.Presentation != nil {
		if err := v.Presentation.Validate(); err != nil {
			return err
		}
	}
	if err := required(v.ID, v.ScopeID); err != nil {
		return err
	}
	if !oneOf(v.Kind, DecisionBusiness, DecisionNativeRequest) {
		return fmt.Errorf("invalid decision kind")
	}
	if v.Owner.Kind == ActorSystem {
		return fmt.Errorf("system cannot own a participation decision")
	}
	if err := v.Owner.Validate(); err != nil {
		return err
	}
	for _, actor := range v.AllowedActors {
		if actor.Kind == ActorSystem {
			return fmt.Errorf("system cannot supply human or delegated consent")
		}
		if err := actor.Validate(); err != nil {
			return err
		}
	}
	if err := ValidateDecisionTransition(v.State, v.State); err != nil {
		return err
	}
	if err := positive("request revision", v.Revision); err != nil {
		return err
	}
	if err := positive("request generation", v.RequestGeneration); err != nil {
		return err
	}
	if v.Kind == DecisionNativeRequest && v.AttemptID == "" {
		return fmt.Errorf("native request requires attempt")
	}
	if (v.AgreementID == "") != (v.AgreementRevision == 0) || v.AgreementRevision < 0 {
		return fmt.Errorf("agreement reference requires positive revision")
	}
	if v.State == DecisionSuperseded && (v.SupersededBy == "" || v.SupersededBy == v.ID) {
		return fmt.Errorf("superseded request requires replacement")
	}
	return nil
}

type Decision struct {
	ID                string    `json:"id"`
	RequestID         string    `json:"request_id"`
	RequestRevision   int64     `json:"request_revision"`
	RequestGeneration int64     `json:"request_generation"`
	AgreementRevision int64     `json:"agreement_revision"`
	Actor             Actor     `json:"actor"`
	Choice            string    `json:"choice"`
	Source            SourceRef `json:"source"`
	CreatedAt         time.Time `json:"created_at"`
}

func ValidateDecision(request DecisionRequest, decision Decision) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := required(decision.ID, decision.RequestID, decision.Choice); err != nil {
		return err
	}
	if err := decision.Actor.Validate(); err != nil {
		return err
	}
	if err := decision.Source.Validate(); err != nil {
		return err
	}
	if request.State != DecisionOpen {
		return fmt.Errorf("request is no longer open")
	}
	if decision.RequestID != request.ID || decision.RequestRevision != request.Revision || decision.RequestGeneration != request.RequestGeneration || decision.AgreementRevision != request.AgreementRevision {
		return fmt.Errorf("stale decision reference")
	}
	if request.Presentation != nil {
		offered := false
		for _, choice := range request.Presentation.Choices {
			if choice.Value == decision.Choice {
				offered = true
				break
			}
		}
		if !offered {
			return fmt.Errorf("decision choice was not offered in this generation")
		}
	}
	for _, actor := range request.AllowedActors {
		if actor == decision.Actor {
			return nil
		}
	}
	return fmt.Errorf("actor is not authorized to decide")
}

type AgreementSubjectKind string

const (
	AgreementItem     AgreementSubjectKind = "item"
	AgreementCategory AgreementSubjectKind = "category"
	AgreementProject  AgreementSubjectKind = "project"
	AgreementPhase    AgreementSubjectKind = "phase"
)

type AgreementValidity string

const (
	AgreementOnce       AgreementValidity = "once"
	AgreementPersistent AgreementValidity = "persistent"
)

type ReportingGranularity string

const (
	ReportingAll            ReportingGranularity = "all"
	ReportingMilestones     ReportingGranularity = "milestones"
	ReportingActionRequired ReportingGranularity = "action_required"
)

type ParticipationAgreement struct {
	ID                   string               `json:"id"`
	ScopeID              string               `json:"scope_id"`
	Initiator            Actor                `json:"initiator"`
	AffectedPrincipalIDs []string             `json:"affected_principal_ids"`
	Source               SourceRef            `json:"source"`
	SubjectKind          AgreementSubjectKind `json:"subject_kind"`
	SubjectID            string               `json:"subject_id"`
	DecisionOwner        Actor                `json:"decision_owner"`
	ParticipationPoints  []string             `json:"participation_points"`
	Reporting            ReportingGranularity `json:"reporting"`
	Validity             AgreementValidity    `json:"validity"`
	SupersedesID         string               `json:"supersedes_id,omitempty"`
	Revision             int64                `json:"revision"`
	ValidFrom            time.Time            `json:"valid_from"`
	ValidUntil           *time.Time           `json:"valid_until,omitempty"`
}

func (v ParticipationAgreement) Validate() error {
	if v.Initiator.Kind != ActorPrincipal {
		return fmt.Errorf("participation changes require a human initiator")
	}
	if err := required(v.ID, v.ScopeID, v.SubjectID); err != nil {
		return err
	}
	for _, actor := range []Actor{v.Initiator, v.DecisionOwner} {
		if err := actor.Validate(); err != nil {
			return err
		}
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	if !oneOf(v.SubjectKind, AgreementItem, AgreementCategory, AgreementProject, AgreementPhase) || !oneOf(v.Validity, AgreementOnce, AgreementPersistent) || !oneOf(v.Reporting, ReportingAll, ReportingMilestones, ReportingActionRequired) {
		return fmt.Errorf("invalid participation agreement enum")
	}
	if v.SupersedesID == v.ID {
		return fmt.Errorf("agreement cannot supersede itself")
	}
	if v.ValidUntil != nil && !v.ValidUntil.After(v.ValidFrom) {
		return fmt.Errorf("agreement validity must end after start")
	}
	return positive("agreement revision", v.Revision)
}

// Reporting-only edits cannot silently transfer decision responsibility.
func ValidateReportingChange(before, after ParticipationAgreement) error {
	if err := before.Validate(); err != nil {
		return err
	}
	if err := after.Validate(); err != nil {
		return err
	}
	if before.ID != after.ID || before.ScopeID != after.ScopeID || before.DecisionOwner != after.DecisionOwner || before.SubjectKind != after.SubjectKind || before.SubjectID != after.SubjectID || before.Validity != after.Validity {
		return fmt.Errorf("reporting change alters agreement authority or scope")
	}
	if after.Revision != before.Revision+1 {
		return fmt.Errorf("agreement revision must advance exactly once")
	}
	return nil
}
