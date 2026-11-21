package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ProvisionStep struct {
	ID          string              `json:"id"`
	ScopeID     string              `json:"scope_id"`
	OperationID string              `json:"operation_id"`
	StepID      string              `json:"step_id"`
	Identity    work.RoleBotBinding `json:"identity"`
	State       work.OperationState `json:"state"`
	Intent      json.RawMessage     `json:"intent"`
	Receipt     json.RawMessage     `json:"receipt,omitempty"`
	ExternalID  string              `json:"external_id,omitempty"`
	Version     string              `json:"version,omitempty"`
	Blocker     string              `json:"blocker,omitempty"`
	Revision    int64               `json:"revision"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

func ProvisionStepID(operationID, stepID string) string {
	sum := sha256.Sum256([]byte(operationID + "\x00" + stepID))
	return "provision-step_" + hex.EncodeToString(sum[:])
}
func (v ProvisionStep) Validate() error {
	if v.ID != ProvisionStepID(v.OperationID, v.StepID) || v.ScopeID == "" || v.OperationID == "" || v.StepID == "" || v.Revision < 1 || !json.Valid(v.Intent) {
		return fmt.Errorf("invalid provisioning step identity or intent")
	}
	if err := v.Identity.Validate(); err != nil {
		return err
	}
	if err := work.ValidateOperationTransition(v.State, v.State); err != nil {
		return err
	}
	if len(v.Receipt) > 0 && !json.Valid(v.Receipt) {
		return fmt.Errorf("invalid provisioning receipt")
	}
	if v.State == work.OperationConfirmed && len(v.Receipt) == 0 {
		return fmt.Errorf("confirmed provisioning step needs receipt")
	}
	return nil
}
func (t *Tx) PutProvisionStep(v ProvisionStep, expected int64) error {
	operation, err := t.GetOperation(v.OperationID)
	if err != nil {
		return err
	}
	if operation.ScopeID != v.ScopeID || (operation.Kind != "project.provision" && operation.Kind != "project.member.provision") {
		return ErrScopeDenied
	}
	if expected > 0 {
		old, err := t.GetProvisionStep(v.OperationID, v.StepID)
		if err != nil {
			return err
		}
		if old.ScopeID != v.ScopeID || old.Identity != v.Identity || string(old.Intent) != string(v.Intent) {
			return fmt.Errorf("provisioning intent identity is immutable")
		}
		if old.ExternalID != "" && v.ExternalID != old.ExternalID {
			return fmt.Errorf("cannot replace an already observed external object")
		}
		if err := work.ValidateOperationTransition(old.State, v.State); err != nil {
			return err
		}
	}
	return t.record("provision_steps", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetProvisionStep(operationID, stepID string) (ProvisionStep, error) {
	var v ProvisionStep
	err := t.read("provision_steps", ProvisionStepID(operationID, stepID), &v)
	return v, err
}
func (t *Tx) ListProvisionSteps(operationID string) ([]ProvisionStep, error) {
	return listRecords[ProvisionStep](t, "provision_steps", " WHERE operation_id=?", operationID)
}

type ProjectHome struct {
	DirectoryGeneration        int64               `json:"directory_generation,omitempty"`
	DirectoryAppliedGeneration int64               `json:"directory_applied_generation,omitempty"`
	DirectorySelectionRef      string              `json:"directory_selection_ref,omitempty"`
	ID                         string              `json:"id"`
	ScopeID                    string              `json:"scope_id"`
	OperationID                string              `json:"operation_id"`
	ChannelGeneration          int64               `json:"channel_generation"`
	Identity                   work.RoleBotBinding `json:"identity"`
	ContainerKind              string              `json:"container_kind,omitempty"`
	ContainerID                string              `json:"container_id,omitempty"`
	DocumentID                 string              `json:"document_id,omitempty"`
	ResourceID                 string              `json:"resource_id,omitempty"`
	URL                        string              `json:"url,omitempty"`
	DocumentRevision           int                 `json:"document_revision,omitempty"`
	IndexBlockID               string              `json:"index_block_id,omitempty"`
	StatusBlockID              string              `json:"status_block_id,omitempty"`
	IndexBaseline              string              `json:"index_baseline,omitempty"`
	StatusBaseline             string              `json:"status_baseline,omitempty"`
	TabID                      string              `json:"tab_id,omitempty"`
	NavigationMessageID        string              `json:"navigation_message_id,omitempty"`
	PinMessageID               string              `json:"pin_message_id,omitempty"`
	SyncState                  string              `json:"sync_state"`
	Revision                   int64               `json:"revision"`
	UpdatedAt                  time.Time           `json:"updated_at"`
}

func (v ProjectHome) Validate() error {
	if v.ID == "" || v.ScopeID == "" || v.OperationID == "" || v.ChannelGeneration < 1 || v.Revision < 1 {
		return fmt.Errorf("invalid project home ownership")
	}
	if err := v.Identity.Validate(); err != nil {
		return err
	}
	switch v.SyncState {
	case "pending", "synced", "conflict", "unknown":
	default:
		return fmt.Errorf("invalid project home sync state")
	}
	return nil
}
func (t *Tx) PutProjectHome(v ProjectHome, expected int64) error {
	project, err := t.GetProject(v.ID)
	if err != nil {
		return err
	}
	operation, err := t.GetOperation(v.OperationID)
	if err != nil {
		return err
	}
	if project.ScopeID != v.ScopeID || operation.Kind != "project.provision" || operation.TargetID != project.ID || operation.TargetGeneration != v.ChannelGeneration {
		return ErrScopeDenied
	}
	var previous *ProjectHome
	if expected > 0 {
		old, err := t.GetProjectHome(v.ID)
		if err != nil {
			return err
		}
		previous = &old
		if old.OperationID != v.OperationID || old.ScopeID != v.ScopeID || old.Identity != v.Identity || old.ChannelGeneration != v.ChannelGeneration {
			return fmt.Errorf("project home ownership is immutable")
		}
		if old.DocumentID != "" && old.DocumentID != v.DocumentID {
			return fmt.Errorf("stable project document cannot be replaced")
		}
		if old.IndexBlockID != "" && old.IndexBlockID != v.IndexBlockID || old.StatusBlockID != "" && old.StatusBlockID != v.StatusBlockID {
			return fmt.Errorf("managed project blocks cannot be silently replaced")
		}
	}
	if err = t.validateProjectHomeDirectory(v, previous, operation); err != nil {
		return err
	}
	return t.record("project_homes", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetProjectHome(projectID string) (ProjectHome, error) {
	var v ProjectHome
	err := t.read("project_homes", projectID, &v)
	return v, err
}
