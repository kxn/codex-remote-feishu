package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const SchemaPersonalMigrationReactionHoldV1 = `CREATE TABLE IF NOT EXISTS personal_migration_reaction_holds(scope_id TEXT NOT NULL REFERENCES personal_migration_fences(scope_id),reaction_id TEXT PRIMARY KEY REFERENCES team_private_reactions(id),reaction_revision INTEGER NOT NULL,reaction_payload TEXT NOT NULL,intake_proof_id TEXT NOT NULL,evidence_hash TEXT NOT NULL,created_at TEXT NOT NULL);`

// HoldPersonalUncertainReaction freezes an uncertain source presentation chain.
// It is not a platform receipt and never authorizes another HTTP write.
func (t *Tx) HoldPersonalUncertainReaction(scopeID, reactionID, proofID string, now time.Time) error {
	if now.IsZero() || proofID == "" {
		return ErrConflict
	}
	fence, err := t.GetPersonalMigrationFence(scopeID)
	if err != nil {
		return err
	}
	if fence.State != "draining" {
		return ErrConflict
	}
	proof, err := t.GetContent(scopeID, proofID)
	if err != nil {
		return err
	}
	var intake struct {
		MigrationID string `json:"migration_id"`
		Digest      string `json:"digest"`
	}
	if json.Unmarshal(proof.Data, &intake) != nil || intake.MigrationID != fence.MigrationID || intake.Digest == "" || proofID != "migration-intake-proof:"+intake.Digest {
		return ErrConflict
	}
	reaction, err := t.GetPrivateReaction(reactionID)
	if err != nil {
		return err
	}
	if reaction.ScopeID != scopeID || reaction.Finished || (reaction.InFlightAction != "create" && reaction.InFlightAction != "delete") || reaction.InFlightEmoji == "" || (reaction.InFlightAction == "delete" && reaction.ReactionIDs[reaction.InFlightEmoji] == "") {
		return ErrConflict
	}
	raw, err := json.Marshal(reaction)
	if err != nil {
		return err
	}
	body, err := json.Marshal([]json.RawMessage{raw, json.RawMessage(proof.Data)})
	if err != nil {
		return err
	}
	hash := sha256.Sum256(body)
	digest := hex.EncodeToString(hash[:])
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO personal_migration_reaction_holds(scope_id,reaction_id,reaction_revision,reaction_payload,intake_proof_id,evidence_hash,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(reaction_id) DO NOTHING`, scopeID, reaction.ID, reaction.Revision, string(raw), proofID, digest, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	var old string
	err = t.tx.QueryRowContext(t.ctx, `SELECT evidence_hash FROM personal_migration_reaction_holds WHERE scope_id=? AND reaction_id=? AND reaction_revision=? AND reaction_payload=?`, scopeID, reaction.ID, reaction.Revision, string(raw)).Scan(&old)
	if err != nil {
		return err
	}
	if old != digest {
		return ErrConflict
	}
	return nil
}

func (t *Tx) validatePersonalReactionHolds(scopeID string) error {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT reaction_payload,intake_proof_id,evidence_hash FROM personal_migration_reaction_holds WHERE scope_id=?`, scopeID)
	if err != nil {
		return err
	}
	type entry struct{ payload, proof, hash string }
	var all []entry
	for rows.Next() {
		var v entry
		if err = rows.Scan(&v.payload, &v.proof, &v.hash); err != nil {
			rows.Close()
			return err
		}
		all = append(all, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range all {
		proof, e := t.GetContent(scopeID, v.proof)
		if e != nil {
			return e
		}
		body, e := json.Marshal([]json.RawMessage{json.RawMessage(v.payload), json.RawMessage(proof.Data)})
		if e != nil {
			return e
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != v.hash {
			return ErrConflict
		}
	}
	return nil
}

func (t *Tx) ListUncertainPersonalReactions(scopeID string) ([]PrivateReaction, error) {
	return listRecords[PrivateReaction](t, "team_private_reactions", " WHERE json_extract(payload,'$.scope_id')=? AND coalesce(json_extract(payload,'$.in_flight_action'),'')<>''", scopeID)
}
