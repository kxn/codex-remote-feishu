package workstore

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ProfileLaunchReference struct {
	ScopeID, WorkerID, OperationID, ProfileID string
	WorkerGeneration, SettingsRevision        int64
	Backend                                   work.Backend
}

// ListActiveProfileLaunchReferences reads exact scope-owned worker.launch
// content in this transaction. Missing or ambiguous launches fail closed.
func (t *Tx) ListActiveProfileLaunchReferences(backend work.Backend, profileID string) ([]ProfileLaunchReference, error) {
	if profileID == "" || (backend != work.BackendCodex && backend != work.BackendClaude && backend != work.BackendOpenCode) {
		return nil, ErrScopeDenied
	}
	scopes, err := t.ListScopes()
	if err != nil {
		return nil, err
	}
	var result []ProfileLaunchReference
	for _, scope := range scopes {
		workers, e := t.ListWorkers(scope.ID)
		if e != nil {
			return nil, e
		}
		attempts, e := t.ListAttempts(scope.ID)
		if e != nil {
			return nil, e
		}
		ops, e := t.ListOperations(scope.ID)
		if e != nil {
			return nil, e
		}
		for _, worker := range workers {
			if worker.Backend != backend {
				continue
			}
			active := worker.State != work.WorkerRetired
			if !active {
				for _, a := range attempts {
					if a.RunRef.WorkerID == worker.ID && (a.State == work.AttemptPrepared || a.State == work.AttemptDispatchPending || a.State == work.AttemptRunning || a.State == work.AttemptReconciling) {
						active = true
						break
					}
				}
			}
			if !active {
				continue
			}
			matches := 0
			var found ProfileLaunchReference
			for _, op := range ops {
				if op.Kind != "worker.launch" || op.TargetID != worker.ID || op.TargetGeneration != worker.Generation {
					continue
				}
				matches++
				if op.ScopeID != scope.ID || op.RequestRef == "" {
					return nil, ErrConflict
				}
				body, e := t.GetContent(scope.ID, op.RequestRef)
				if e != nil {
					return nil, e
				}
				var launch struct {
					WorkerID         string                           `json:"worker_id"`
					ScopeID          string                           `json:"scope_id"`
					Generation       int64                            `json:"generation"`
					ProfileID        string                           `json:"profile_id"`
					SettingsRevision int64                            `json:"settings_revision"`
					Backend          work.Backend                     `json:"backend"`
					Snapshot         agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
				}
				if json.Unmarshal(body.Data, &launch) != nil || launch.WorkerID != worker.ID || launch.ScopeID != scope.ID || launch.Generation != worker.Generation || launch.Backend != worker.Backend {
					return nil, ErrConflict
				}
				if launch.ProfileID == profileID {
					found = ProfileLaunchReference{ScopeID: scope.ID, WorkerID: worker.ID, OperationID: op.ID, ProfileID: profileID, WorkerGeneration: worker.Generation, SettingsRevision: launch.SettingsRevision, Backend: launch.Backend}
				}
			}
			if matches != 1 {
				return nil, fmt.Errorf("active worker launch missing or ambiguous: %w", ErrConflict)
			}
			if found.WorkerID != "" {
				result = append(result, found)
			}
		}
	}
	return result, nil
}
