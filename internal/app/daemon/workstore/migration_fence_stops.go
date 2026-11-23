package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// RequestPersonalMigrationStops records the durable migration reason for each
// occupied attempt without cancelling pending messages or fabricating a human stop.
func (t *Tx) RequestPersonalMigrationStops(scopeID, migrationID string, now time.Time) ([]string, error) {
	fence, err := t.GetPersonalMigrationFence(scopeID)
	if err != nil {
		return nil, err
	}
	if fence.State != "draining" || fence.MigrationID != migrationID || now.IsZero() {
		return nil, ErrConflict
	}
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return nil, err
	}
	if scope.Kind != work.ScopePersonal {
		return nil, ErrScopeDenied
	}
	payload, err := json.Marshal(fence)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(payload)
	evidenceID := "migration-fence:" + hex.EncodeToString(digest[:])
	if err = t.PutContent(work.NewContent(evidenceID, scopeID, "application/json", payload, fence.UpdatedAt)); err != nil {
		return nil, err
	}
	source := work.SourceRef{Kind: "personal_migration", ID: migrationID, Version: fmt.Sprint(fence.Revision), EvidenceRef: evidenceID}
	attempts, err := t.ListOccupiedAttempts(scopeID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		if err = t.RecordPrivateAttemptStop(attempt, scope, source, fence.UpdatedAt, now); err != nil {
			return nil, err
		}
		ids = append(ids, attempt.ID)
	}
	return ids, nil
}
