package work

import (
	"fmt"
	"time"
)

type Backend string

const (
	BackendCodex    Backend = "codex"
	BackendClaude   Backend = "claude"
	BackendOpenCode Backend = "opencode"
)

type WorkerState string

const (
	WorkerPreparing   WorkerState = "preparing"
	WorkerReady       WorkerState = "ready"
	WorkerBusy        WorkerState = "busy"
	WorkerMaintaining WorkerState = "maintaining"
	WorkerRecovering  WorkerState = "recovering"
	WorkerHandoff     WorkerState = "handoff"
	WorkerUnavailable WorkerState = "unavailable"
	WorkerRetired     WorkerState = "retired"
)

type AttemptState string

const (
	AttemptPrepared        AttemptState = "prepared"
	AttemptDispatchPending AttemptState = "dispatch_pending"
	AttemptRunning         AttemptState = "running"
	AttemptCompleted       AttemptState = "completed"
	AttemptInterrupted     AttemptState = "interrupted"
	AttemptFailed          AttemptState = "failed"
	AttemptReconciling     AttemptState = "reconciling"
)

type ControllerKind string

const (
	ControllerAutomatic ControllerKind = "automatic"
	ControllerHuman     ControllerKind = "human"
)

type ContextPurpose string

const (
	PurposeBusiness ContextPurpose = "business"
	PurposeControl  ContextPurpose = "control"
	PurposeReview   ContextPurpose = "review"
)

type WorkContext struct {
	Review                 *ReviewSpec    `json:"review,omitempty"`
	ControlTargetContextID string         `json:"control_target_context_id,omitempty"`
	Purpose                ContextPurpose `json:"purpose,omitempty"`
	ProjectViewScopeID     string         `json:"project_view_scope_id,omitempty"`
	ProjectContextID       string         `json:"project_context_id,omitempty"`
	TaskID                 string         `json:"task_id,omitempty"`
	ID                     string         `json:"id"`
	ScopeID                string         `json:"scope_id"`
	RoleID                 string         `json:"role_id"`
	Workstream             string         `json:"workstream"`
	Checkpoint             string         `json:"checkpoint,omitempty"`
	AgreementRevision      int64          `json:"agreement_revision"`
	Revision               int64          `json:"revision"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

func (v WorkContext) Validate() error {
	if (v.Purpose == PurposeReview) != (v.Review != nil) {
		return fmt.Errorf("review context requires explicit frozen review spec")
	}
	if v.Review != nil {
		if err := v.Review.Validate(); err != nil {
			return err
		}
		if v.TaskID != "" || v.ProjectViewScopeID != "" || v.ControlTargetContextID != "" {
			return fmt.Errorf("review context cannot become a business owner or control view")
		}
	}
	if v.ControlTargetContextID != "" && (v.Purpose != PurposeControl || v.ControlTargetContextID == v.ID) {
		return fmt.Errorf("invalid control target context")
	}
	if !oneOf(v.Purpose, "", PurposeBusiness, PurposeControl, PurposeReview) {
		return fmt.Errorf("invalid context purpose")
	}
	if v.Purpose == PurposeControl && (v.TaskID != "" || v.ProjectViewScopeID != "") {
		return fmt.Errorf("control context cannot own business task or private view")
	}
	if (v.ProjectViewScopeID == "") != (v.ProjectContextID == "") {
		return fmt.Errorf("private project view requires both project scope and context")
	}
	if err := required(v.ID, v.ScopeID, v.RoleID, v.Workstream); err != nil {
		return err
	}
	if v.AgreementRevision < 0 {
		return fmt.Errorf("negative agreement revision")
	}
	return positive("context revision", v.Revision)
}

type WorkerSession struct {
	CarrierIncarnation      string         `json:"carrier_incarnation,omitempty"`
	ID                      string         `json:"id"`
	ContextID               string         `json:"context_id"`
	ScopeID                 string         `json:"scope_id"`
	RoleID                  string         `json:"role_id"`
	Backend                 Backend        `json:"backend"`
	State                   WorkerState    `json:"state"`
	Generation              int64          `json:"generation"`
	NativeNamespace         string         `json:"native_namespace,omitempty"`
	NativeSessionID         string         `json:"native_session_id,omitempty"`
	InstanceID              string         `json:"instance_id,omitempty"`
	InstanceGeneration      int64          `json:"instance_generation"`
	ProcessGeneration       int64          `json:"process_generation"`
	Controller              ControllerKind `json:"controller"`
	ControllerGeneration    int64          `json:"controller_generation"`
	LocationID              string         `json:"location_id,omitempty"`
	LocationGeneration      int64          `json:"location_generation"`
	DesiredSettingsRevision int64          `json:"desired_settings_revision"`
	AppliedSettingsRevision int64          `json:"applied_settings_revision"`
	Revision                int64          `json:"revision"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

func (v WorkerSession) Validate() error {
	if err := required(v.ID, v.ContextID, v.ScopeID, v.RoleID); err != nil {
		return err
	}
	if !oneOf(v.Backend, BackendCodex, BackendClaude, BackendOpenCode) {
		return fmt.Errorf("invalid backend %q", v.Backend)
	}
	if !oneOf(v.Controller, ControllerAutomatic, ControllerHuman) {
		return fmt.Errorf("invalid controller %q", v.Controller)
	}
	if err := ValidateWorkerTransition(v.State, v.State); err != nil {
		return err
	}
	for _, n := range []int64{v.Generation, v.ControllerGeneration, v.Revision} {
		if err := positive("worker generation/revision", n); err != nil {
			return err
		}
	}
	for _, n := range []int64{v.InstanceGeneration, v.ProcessGeneration, v.LocationGeneration, v.DesiredSettingsRevision, v.AppliedSettingsRevision} {
		if n < 0 {
			return fmt.Errorf("negative worker generation/revision")
		}
	}
	if (v.NativeSessionID == "") != (v.NativeNamespace == "") {
		return fmt.Errorf("native session and namespace must be provided together")
	}
	if v.LocationID != "" && v.LocationGeneration < 1 {
		return fmt.Errorf("location requires generation")
	}
	return nil
}
func ValidateBinding(context WorkContext, worker WorkerSession) error {
	if err := context.Validate(); err != nil {
		return err
	}
	if err := worker.Validate(); err != nil {
		return err
	}
	if context.ID != worker.ContextID || context.ScopeID != worker.ScopeID || context.RoleID != worker.RoleID {
		return fmt.Errorf("worker context/scope/role binding mismatch")
	}
	return nil
}

// RunRef is frozen when an attempt is admitted. Late receipts must match all
// fields; a matching worker ID alone cannot authorize a result.
type RunRef struct {
	WorkerID   string `json:"worker_id"`
	Generation int64  `json:"generation"`
	RunID      string `json:"run_id"`
}

func (v RunRef) Validate() error {
	if err := required(v.WorkerID, v.RunID); err != nil {
		return err
	}
	return positive("run generation", v.Generation)
}

type ExecutionAttempt struct {
	FrozenRequirementsRevision int64        `json:"frozen_requirements_revision"`
	FrozenAssignmentGeneration int64        `json:"frozen_assignment_generation"`
	ID                         string       `json:"id"`
	ContextID                  string       `json:"context_id"`
	ScopeID                    string       `json:"scope_id"`
	TaskID                     string       `json:"task_id,omitempty"`
	RunRef                     RunRef       `json:"run_ref"`
	State                      AttemptState `json:"state"`
	FrozenRefs                 []SourceRef  `json:"frozen_refs"`
	InputIDs                   []string     `json:"input_ids"`
	CommandID                  string       `json:"command_id"`
	TerminalEvidence           []SourceRef  `json:"terminal_evidence,omitempty"`
	Revision                   int64        `json:"revision"`
	CreatedAt                  time.Time    `json:"created_at"`
	UpdatedAt                  time.Time    `json:"updated_at"`
}

func (v ExecutionAttempt) Validate() error {
	if v.TaskID != "" && (v.FrozenRequirementsRevision < 1 || v.FrozenAssignmentGeneration < 1) {
		return fmt.Errorf("task attempt requires frozen requirements and assignment")
	}
	if v.TaskID == "" && (v.FrozenRequirementsRevision != 0 || v.FrozenAssignmentGeneration != 0) {
		return fmt.Errorf("taskless attempt cannot claim task revisions")
	}
	if err := required(v.ID, v.ContextID, v.ScopeID, v.CommandID); err != nil {
		return err
	}
	if err := v.RunRef.Validate(); err != nil {
		return err
	}
	if err := ValidateAttemptTransition(v.State, v.State); err != nil {
		return err
	}
	if err := positive("attempt revision", v.Revision); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range v.InputIDs {
		if err := required(id); err != nil {
			return err
		}
		if seen[id] {
			return fmt.Errorf("duplicate attempt input %q", id)
		}
		seen[id] = true
	}
	for _, refs := range [][]SourceRef{v.FrozenRefs, v.TerminalEvidence} {
		for _, ref := range refs {
			if err := ref.Validate(); err != nil {
				return err
			}
		}
	}
	if oneOf(v.State, AttemptCompleted, AttemptInterrupted, AttemptFailed) && len(v.TerminalEvidence) == 0 {
		return fmt.Errorf("terminal attempt requires evidence")
	}
	return nil
}
func ValidateAttemptBinding(attempt ExecutionAttempt, worker WorkerSession) error {
	if err := attempt.Validate(); err != nil {
		return err
	}
	if err := worker.Validate(); err != nil {
		return err
	}
	if attempt.RunRef.WorkerID != worker.ID || attempt.RunRef.Generation != worker.Generation || attempt.ContextID != worker.ContextID || attempt.ScopeID != worker.ScopeID {
		return fmt.Errorf("attempt binding is stale or belongs to another worker")
	}
	return nil
}
