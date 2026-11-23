package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Caller holds the profile reference gate, before any a.mu acquisition.
func (a *App) privateCodexReferenceSnapshot() (privatechat.RuntimeStatus, []privatechat.CodexReference, error) {
	var status privatechat.RuntimeStatus
	if a.applications == nil || a.applications.processor == nil || a.applications.journal == nil || a.applications.native == nil || a.applications.native.store == nil {
		return status, nil, fmt.Errorf("private profile references unavailable")
	}
	var err error
	status, err = a.applications.processor.ReadRuntimeStatus(context.Background())
	if err != nil {
		return status, nil, err
	}
	refs, err := a.applications.journal.CodexReferences(context.Background())
	if err != nil {
		return status, nil, err
	}
	rows, err := a.applications.native.store.db.QueryContext(context.Background(), `SELECT record,state FROM carriers WHERE state NOT IN ('retired','aborted') ORDER BY instance`)
	if err != nil {
		return status, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var stateName string
		if err = rows.Scan(&raw, &stateName); err != nil {
			return status, nil, err
		}
		var rec privateCarrierRecord
		if err = json.Unmarshal(raw, &rec); err != nil {
			return status, nil, err
		}
		if rec.State != stateName {
			return status, nil, fmt.Errorf("corrupt private carrier state")
		}
		frozen, e := json.Marshal(rec.Snapshot)
		if e != nil {
			return status, nil, e
		}
		sum := sha256.Sum256(frozen)
		if rec.SnapshotHash != hex.EncodeToString(sum[:]) {
			return status, nil, fmt.Errorf("corrupt private carrier snapshot")
		}

		if ref := state.NormalizeCodexAdmissionRef(rec.CodexAdmissionRef); ref != nil {
			refs = append(refs, privatechat.CodexReference{SurfaceID: rec.Surface.SurfaceID, Kind: "private_carrier", Name: rec.Snapshot.Owner.InstanceID, Admission: *ref})
		}
	}
	if err = rows.Err(); err != nil {
		return status, nil, err
	}
	return status, refs, nil
}

func (a *App) teamCodexProfileReferences(profileID string) ([]workstore.ProfileLaunchReference, error) {
	if a.teamRuntime == nil || a.teamRuntime.store == nil {
		return nil, nil
	}
	var refs []workstore.ProfileLaunchReference
	err := a.teamRuntime.store.Within(context.Background(), func(tx *workstore.Tx) error {
		var err error
		refs, err = tx.ListActiveProfileLaunchReferences(work.BackendCodex, profileID)
		return err
	})
	return refs, err
}
