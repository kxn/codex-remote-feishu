package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const publicationGrantSchema = `CREATE TABLE IF NOT EXISTS publication_grants (
 id TEXT PRIMARY KEY NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),payload TEXT NOT NULL CHECK(json_valid(payload)),
 scope_id TEXT GENERATED ALWAYS AS (json_extract(payload,'$.scope_id')) STORED NOT NULL REFERENCES scopes(id),
 role_id TEXT GENERATED ALWAYS AS (json_extract(payload,'$.grantee.role_id')) STORED NOT NULL REFERENCES roles(id));
 CREATE INDEX IF NOT EXISTS publication_grants_by_grantee ON publication_grants(scope_id,role_id);`

func (t *Tx) initPublicationGrants() error {
	_, err := t.tx.ExecContext(t.ctx, publicationGrantSchema)
	return err
}

func (t *Tx) CreatePublicationGrant(v work.PublicationGrant) error {
	if v.Revoked {
		return ErrConflict
	}
	return t.record("publication_grants", v.ID, v.Revision, 0, v)
}

func (t *Tx) RevokePublicationGrant(scopeID, id string) (work.PublicationGrant, error) {
	v, err := t.GetPublicationGrant(scopeID, id)
	if err != nil || v.Revoked {
		return v, err
	}
	old := v.Revision
	v.Revoked, v.Revision = true, old+1
	return v, t.record("publication_grants", v.ID, v.Revision, old, v)
}

func (t *Tx) GetPublicationGrant(scopeID, id string) (work.PublicationGrant, error) {
	var v work.PublicationGrant
	err := t.read("publication_grants", id, &v)
	if err == nil && v.ScopeID != scopeID {
		return work.PublicationGrant{}, ErrScopeDenied
	}
	return v, err
}

func (t *Tx) ListPublicationGrants(scopeID, roleID string) ([]work.PublicationGrant, error) {
	return listRecords[work.PublicationGrant](t, "publication_grants", " WHERE scope_id=? AND role_id=? AND json_extract(payload,'$.revoked')=0", scopeID, roleID)
}
