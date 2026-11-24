package daemon

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// An unresolved request may be retained within an already verified execution
// component only when its full native binding names that exact attempt/carrier.
func verifiedPersonalRequestComponents(rows map[string][]workstore.PersonalExportRecord, heldAttempts map[string]bool) (map[string]bool, error) {
	attempts := map[string]work.ExecutionAttempt{}
	for _, row := range rows["attempts"] {
		var a work.ExecutionAttempt
		if json.Unmarshal(row.Payload, &a) != nil {
			return nil, fmt.Errorf("invalid request attempt")
		}
		attempts[row.Key] = a
	}
	workers := map[string]work.WorkerSession{}
	for _, row := range rows["workers"] {
		var w work.WorkerSession
		if json.Unmarshal(row.Payload, &w) != nil {
			return nil, fmt.Errorf("invalid request worker")
		}
		workers[row.Key] = w
	}
	carriers := map[string]workstore.CarrierRegistration{}
	for _, row := range rows["carrier_registrations"] {
		var stored struct {
			Registration workstore.CarrierRegistration `json:"registration"`
		}
		if json.Unmarshal(row.Payload, &stored) != nil || stored.Registration.Incarnation != row.Key {
			return nil, fmt.Errorf("invalid request carrier")
		}
		carriers[row.Key] = stored.Registration
	}
	out := map[string]bool{}
	for _, row := range rows["native_ui_requests"] {
		var req workstore.NativeRequest
		if json.Unmarshal(row.Payload, &req) != nil || req.ID != row.Key {
			return nil, fmt.Errorf("invalid native request component")
		}
		if !heldAttempts[req.Binding.AttemptID] {
			continue
		}
		a, ok := attempts[req.Binding.AttemptID]
		if !ok {
			continue
		}
		w, ok := workers[a.RunRef.WorkerID]
		if !ok {
			continue
		}
		b := req.Binding
		carrier, ok := carriers[req.CarrierIncarnation]
		if !ok || !carrier.Matches(w) || !carrier.Owner.SameOwner(b) || b.ValidateExecution() != nil || req.ScopeID != a.ScopeID || b.ScopeID != a.ScopeID || b.ContextID != a.ContextID || b.RoleID != w.RoleID || b.WorkerSessionID != w.ID || b.SessionGeneration != a.RunRef.Generation || b.SessionGeneration != w.Generation || b.ControllerGeneration != w.ControllerGeneration || b.RunRef != a.RunRef.RunID || req.CarrierIncarnation != w.CarrierIncarnation {
			continue
		}
		out[row.Key] = true
	}
	return out, nil
}
