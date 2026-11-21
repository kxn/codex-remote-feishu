package work

import (
	"fmt"
	"time"
)

type InputAdmissionState string

const (
	InputAdmissionSending         InputAdmissionState = "sending"
	InputAdmissionUnknown         InputAdmissionState = "unknown"
	InputAdmissionConfirmed       InputAdmissionState = "confirmed"
	InputAdmissionRejected        InputAdmissionState = "rejected"
	InputAdmissionQuiescedUnknown InputAdmissionState = "quiesced_unknown"
)

type AttemptInputAdmission struct {
	ID                string              `json:"id"`
	CommandID         string              `json:"command_id"`
	ScopeID           string              `json:"scope_id"`
	ContextID         string              `json:"context_id"`
	AttemptID         string              `json:"attempt_id"`
	RunRef            RunRef              `json:"run_ref"`
	NativeSessionID   string              `json:"native_session_id"`
	NativeTurnID      string              `json:"native_turn_id"`
	InputIDs          []string            `json:"input_ids"`
	Source            SourceRef           `json:"source"`
	State             InputAdmissionState `json:"state"`
	Quiescence        *QuiescenceEvidence `json:"quiescence,omitempty"`
	LateNativeSources []SourceRef         `json:"late_native_sources,omitempty"`
	NativeSource      *SourceRef          `json:"native_source,omitempty"`
	Revision          int64               `json:"revision"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
}

func (v AttemptInputAdmission) Validate() error {
	if err := required(v.ID, v.CommandID, v.ScopeID, v.ContextID, v.AttemptID, v.NativeSessionID, v.NativeTurnID); err != nil {
		return err
	}
	if err := v.RunRef.Validate(); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	if !oneOf(v.State, InputAdmissionSending, InputAdmissionUnknown, InputAdmissionConfirmed, InputAdmissionRejected, InputAdmissionQuiescedUnknown) || v.Revision < 1 || v.CreatedAt.IsZero() || v.UpdatedAt.IsZero() || len(v.InputIDs) == 0 {
		return fmt.Errorf("invalid native input admission")
	}
	seen := map[string]bool{}
	for _, id := range v.InputIDs {
		if id == "" || seen[id] {
			return fmt.Errorf("admission requires unique exact inputs")
		}
		seen[id] = true
	}
	if v.NativeSource != nil {
		if err := v.NativeSource.Validate(); err != nil {
			return err
		}
	}
	if (v.State == InputAdmissionConfirmed || v.State == InputAdmissionRejected) && v.NativeSource == nil {
		return fmt.Errorf("native input admission requires actual result evidence")
	}
	if v.State == InputAdmissionQuiescedUnknown {
		if v.Quiescence == nil || v.Quiescence.RunRef != v.RunRef {
			return fmt.Errorf("uncertain input requires matching stop evidence")
		}
		return v.Quiescence.Validate()
	}
	return nil
}
