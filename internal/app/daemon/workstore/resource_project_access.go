package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const ProjectDocumentShareKind = "resource.document.share-project"
const ProjectDocumentAccessObservationKind = "resource.document.project-access.observe"

type ProjectDocumentAccessObservation struct {
	Permission string    `json:"permission,omitempty"`
	ReadFailed bool      `json:"read_failed,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

// CurrentProjectDocumentAccess is a receipt for this channel generation only.
// Unmanaged external documents retain their existing platform permissions.
func (t *Tx) CurrentProjectDocumentAccess(resource work.Resource, tenant, chat string, generation int64) (managed, confirmed bool, err error) {
	if generation < 1 || !strings.HasPrefix(resource.SourceIdentity, "docx:") {
		return false, false, nil
	}
	if _, err = t.ProjectDocumentCreation(resource.ScopeID, resource.ID); errors.Is(err, ErrNotFound) {
		return false, false, nil
	} else if err != nil {
		return false, false, err
	}
	id := ProjectDocumentShareID(resource.ScopeID, resource.ID, tenant, chat, generation)
	latest, err := t.LatestProjectDocumentAccessObservation(resource.ScopeID, id)
	if errors.Is(err, ErrNotFound) {
		return true, false, nil
	}
	if err != nil {
		return true, false, err
	}
	if latest.State != work.OperationConfirmed {
		return true, false, nil
	}
	var observed ProjectDocumentAccessObservation
	err = json.Unmarshal([]byte(latest.ExternalReceipt), &observed)
	return true, !observed.ReadFailed && observed.Permission != "", err
}

func (t *Tx) LatestProjectDocumentAccessObservation(scopeID, target string) (work.Operation, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id FROM operations WHERE scope_id=? AND kind=? AND json_extract(payload,'$.target_id')=? ORDER BY json_extract(payload,'$.target_generation') DESC, rowid DESC LIMIT 1`, scopeID, ProjectDocumentAccessObservationKind, target).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return work.Operation{}, ErrNotFound
	}
	if err != nil {
		return work.Operation{}, err
	}
	return t.GetOperation(id)
}

// The initial authorization and every explicit recovery have separate immutable
// logical IDs. Include a pending restore so a second new command cannot race it.
func (t *Tx) LatestProjectDocumentShare(scopeID, base string) (work.Operation, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id FROM operations WHERE scope_id=? AND kind=? AND (id=? OR substr(id,1,length(?))=?) ORDER BY rowid DESC LIMIT 1`, scopeID, ProjectDocumentShareKind, base, base+":restore:", base+":restore:").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return work.Operation{}, ErrNotFound
	}
	if err != nil {
		return work.Operation{}, err
	}
	return t.GetOperation(id)
}

func ProjectDocumentShareID(scopeID, resourceID, tenant, chat string, generation int64) string {
	payload, _ := json.Marshal([]any{scopeID, resourceID, tenant, chat, generation})
	hash := sha256.Sum256(payload)
	return "project-document-share_" + hex.EncodeToString(hash[:])
}

func (t *Tx) ProjectDocumentCreation(scopeID, resourceID string) (work.Operation, error) {
	records, err := listRecords[work.Operation](t, "operations", ` WHERE scope_id=? AND kind='resource.document.create' AND state='confirmed' AND json_extract(CASE WHEN json_valid(json_extract(payload,'$.external_receipt')) THEN json_extract(payload,'$.external_receipt') ELSE '{}' END,'$.object_id')=?`, scopeID, resourceID)
	if err != nil {
		return work.Operation{}, err
	}
	if len(records) == 0 {
		return work.Operation{}, ErrNotFound
	}
	if len(records) != 1 {
		return work.Operation{}, ErrConflict
	}
	return records[0], nil
}
