package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// teamHistoricalSession returns true for an exact known team history. A legacy
// launch without a frozen namespace remains ambiguous for that exact backend
// and session and is denied until source evidence supplies its namespace.
func teamHistoricalSession(ctx context.Context, store *workstore.Store, backend agentproto.Backend, namespace, session string) (bool, error) {
	if store == nil || namespace == "" || session == "" {
		return false, workstore.ErrConflict
	}
	var restricted bool
	err := store.Within(ctx, func(tx *workstore.Tx) error {
		carriers, e := tx.HistoricalSessionCarriers(session)
		if e != nil {
			return e
		}
		for _, carrier := range carriers {
			if string(carrier.Backend) != string(backend) {
				continue
			}
			op, e := tx.GetOperation(workhost.LaunchOperationID(carrier.Owner.WorkerSessionID, carrier.Owner.SessionGeneration))
			if errors.Is(e, workstore.ErrNotFound) {
				restricted = true
				continue
			}
			if e != nil {
				return e
			}
			body, e := tx.GetContent(carrier.Owner.ScopeID, op.RequestRef)
			if e != nil {
				return e
			}
			var launch workhost.LaunchRecord
			if json.Unmarshal(body.Data, &launch) != nil || launch.WorkerID != carrier.Owner.WorkerSessionID || launch.Generation != carrier.Owner.SessionGeneration || launch.Snapshot.CarrierIncarnation != carrier.Incarnation || launch.Backend != carrier.Backend {
				return workstore.ErrConflict
			}
			if launch.NativeHistoryNamespace == "" || launch.NativeHistoryNamespace == namespace {
				restricted = true
			}
		}
		return nil
	})
	return restricted, err
}

// teamHistoryPolicySnapshot captures only committed SDK/native history facts;
// missing launch namespaces are retained as backend+session ambiguity.
func teamHistoryPolicySnapshot(ctx context.Context, store *workstore.Store) ([]orchestrator.PrivateHistoryKey, []orchestrator.PrivateHistoryKey, error) {
	var denied, ambiguous []orchestrator.PrivateHistoryKey
	if store == nil {
		return nil, nil, workstore.ErrConflict
	}
	err := store.Within(ctx, func(tx *workstore.Tx) error {
		ids, e := tx.HistoricalSessionIDs()
		if e != nil {
			return e
		}
		for _, session := range ids {
			carriers, e := tx.HistoricalSessionCarriers(session)
			if e != nil {
				return e
			}
			for _, carrier := range carriers {
				backend := agentproto.Backend(carrier.Backend)
				op, e := tx.GetOperation(workhost.LaunchOperationID(carrier.Owner.WorkerSessionID, carrier.Owner.SessionGeneration))
				if errors.Is(e, workstore.ErrNotFound) {
					ambiguous = append(ambiguous, orchestrator.PrivateHistoryKey{Backend: backend, Session: session})
					continue
				}
				if e != nil {
					return e
				}
				body, e := tx.GetContent(carrier.Owner.ScopeID, op.RequestRef)
				if e != nil {
					return e
				}
				var launch workhost.LaunchRecord
				if json.Unmarshal(body.Data, &launch) != nil || launch.WorkerID != carrier.Owner.WorkerSessionID || launch.Generation != carrier.Owner.SessionGeneration || launch.Snapshot.CarrierIncarnation != carrier.Incarnation || launch.Backend != carrier.Backend {
					return workstore.ErrConflict
				}
				key := orchestrator.PrivateHistoryKey{Backend: backend, Namespace: launch.NativeHistoryNamespace, Session: session}
				if key.Namespace == "" {
					ambiguous = append(ambiguous, key)
				} else {
					denied = append(denied, key)
				}
			}
		}
		return nil
	})
	return denied, ambiguous, err
}
