package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PersonalMigrationIngressObservation returns only the exact frozen SDK event
// and personal route; identity and neutral-owner proof are verified separately.
func (t *Tx) PersonalMigrationIngressObservation(scopeID, appID, eventID string, generation int64) (work.IngressRecord, work.IngressObservation, error) {
	var record work.IngressRecord
	var observation work.IngressObservation
	var id, raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT ingress_id,payload FROM ingress_observations WHERE app_id=? AND generation=? AND event_id=?`, appID, generation, eventID).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return record, observation, ErrNotFound
	}
	if err != nil {
		return record, observation, err
	}
	if err = json.Unmarshal([]byte(raw), &observation); err != nil {
		return record, observation, err
	}
	record, err = t.GetIngress(id)
	if err != nil {
		return record, observation, err
	}
	if record.Route.ScopeID != scopeID || record.Envelope.ChatType != "p2p" {
		return work.IngressRecord{}, work.IngressObservation{}, ErrScopeDenied
	}
	return record, observation, nil
}

func (t *Tx) PersonalMigrationOpenIDMatches(scopeID, tenant, appID, openID string, generation int64) (bool, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM external_identities x JOIN scopes s ON s.principal_id=x.principal_id WHERE s.id=? AND s.kind='personal' AND x.tenant=? AND x.domain=? AND x.id_type='open_id' AND x.external_id=? AND x.generation=?`, scopeID, tenant, appID, openID, generation).Scan(&count)
	return count == 1, err
}
