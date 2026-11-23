package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPrivateProjectIdentityV1 = `CREATE TABLE IF NOT EXISTS private_project_identity_observations(tenant TEXT NOT NULL,app_id TEXT NOT NULL,event_id TEXT NOT NULL,fact_hash TEXT NOT NULL,owner_id TEXT NOT NULL,owner_epoch INTEGER NOT NULL,identity BLOB NOT NULL,principal_id TEXT NOT NULL REFERENCES principals(id),PRIMARY KEY(tenant,app_id,event_id));`

type PrivateProjectIdentityObservation struct {
	Tenant, AppID, EventID, FactHash, OwnerID string
	OwnerEpoch                                int64
	Generation                                int64
	IDs                                       work.SenderIDs
	AliasIDs                                  work.SenderIDs
	UnionDomain                               string
	ContactOpenID, ContactUserID              string
}

// PrivateProjectIdentityObservationForEvent returns the frozen result of the
// first trusted SDK/contact observation so later API changes cannot rewrite it.
func (t *Tx) PrivateProjectIdentityObservationForEvent(tenant, appID, eventID string) (PrivateProjectIdentityObservation, error) {
	var observation PrivateProjectIdentityObservation
	var raw []byte
	err := t.tx.QueryRowContext(t.ctx, `SELECT identity FROM private_project_identity_observations WHERE tenant=? AND app_id=? AND event_id=?`, tenant, appID, eventID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return observation, ErrNotFound
	}
	if err != nil {
		return observation, err
	}
	if json.Unmarshal(raw, &observation) != nil || observation.Tenant != tenant || observation.AppID != appID || observation.EventID != eventID {
		return observation, ErrConflict
	}
	return observation, nil
}

// ResolvePrivateProjectIdentity is a storage primitive for an already verified
// neutral SDK observation. Callers must obtain the observation from the trusted
// router and verify its frozen private owner; user/model claims are not evidence.
func (t *Tx) ResolvePrivateProjectIdentity(v PrivateProjectIdentityObservation) (work.Principal, error) {
	var result work.Principal
	if v.Tenant == "" || v.AppID == "" || v.EventID == "" || v.FactHash == "" || v.OwnerID == "" || v.OwnerEpoch < 1 || v.Generation < 1 {
		return result, ErrScopeDenied
	}
	ids := v.IDs
	if v.AliasIDs != (work.SenderIDs{}) {
		if v.AliasIDs.UserID == "" || v.AliasIDs.UserID != v.IDs.UserID || v.AliasIDs.OpenID != "" || v.AliasIDs.UnionID != "" {
			return result, ErrScopeDenied
		}
		ids = v.AliasIDs
	}
	evidence := work.IdentityEvidence{Tenant: v.Tenant, AppID: v.AppID, Generation: v.Generation, UnionDomain: v.UnionDomain, IDs: ids, ObservationRef: "private-sdk:" + v.FactHash}
	if err := evidence.Validate(); err != nil {
		return result, err
	}
	raw, _ := json.Marshal(v)
	err := t.atomic(func() error {
		var oldHash, owner, principal string
		var epoch int64
		var old []byte
		err := t.tx.QueryRowContext(t.ctx, `SELECT fact_hash,owner_id,owner_epoch,identity,principal_id FROM private_project_identity_observations WHERE tenant=? AND app_id=? AND event_id=?`, v.Tenant, v.AppID, v.EventID).Scan(&oldHash, &owner, &epoch, &old, &principal)
		if err == nil {
			if oldHash != v.FactHash || owner != v.OwnerID || epoch != v.OwnerEpoch || string(old) != string(raw) {
				return ErrIdentityConflict
			}
			result, err = t.GetPrincipal(principal)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		aliases := identityAliases(evidence)
		if len(aliases) == 0 {
			return ErrScopeDenied
		}
		id := ""
		for _, alias := range aliases {
			existing, e := t.GetExternalIdentity(alias)
			if e != nil && !errors.Is(e, ErrNotFound) {
				return e
			}
			if e == nil {
				if id != "" && id != existing.PrincipalID {
					return ErrIdentityConflict
				}
				id = existing.PrincipalID
			}
		}
		if id == "" {
			sum := sha256.Sum256([]byte(mustJSON(aliases[0])))
			id = "principal:" + hex.EncodeToString(sum[:])
			result = work.Principal{ID: id, DisplayName: aliases[0].ExternalID, Revision: 1}
			if e := t.PutPrincipal(result, 0); e != nil {
				return e
			}
		} else {
			var e error
			result, e = t.GetPrincipal(id)
			if e != nil {
				return e
			}
		}
		for _, alias := range aliases {
			_, e := t.tx.ExecContext(t.ctx, `INSERT INTO external_identities(tenant,domain,id_type,external_id,generation,principal_id,evidence_ref) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, alias.Tenant, alias.AppIdentityDomain, alias.IDType, alias.ExternalID, alias.Generation, id, evidence.ObservationRef)
			if e != nil {
				return e
			}
			actual, e := t.GetExternalIdentity(alias)
			if e != nil {
				return e
			}
			if actual.PrincipalID != id {
				return ErrIdentityConflict
			}
		}
		_, err = t.tx.ExecContext(t.ctx, `INSERT INTO private_project_identity_observations(tenant,app_id,event_id,fact_hash,owner_id,owner_epoch,identity,principal_id) VALUES(?,?,?,?,?,?,?,?)`, v.Tenant, v.AppID, v.EventID, v.FactHash, v.OwnerID, v.OwnerEpoch, raw, id)
		return err
	})
	if err != nil {
		return work.Principal{}, fmt.Errorf("private SDK identity: %w", err)
	}
	return result, nil
}
