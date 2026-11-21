package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaHumanInteractionsV1 = `
CREATE TABLE IF NOT EXISTS human_interactions (
 id TEXT PRIMARY KEY, principal_id TEXT NOT NULL REFERENCES principals(id),
 state TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0),
 payload_hash TEXT NOT NULL, payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS human_interactions_pending ON human_interactions(state,id);
`

type HumanInteraction struct {
	ID          string                        `json:"id"`
	PrincipalID string                        `json:"principal_id"`
	Gateway     work.IngressGateway           `json:"gateway"`
	Envelope    work.HumanInteractionEnvelope `json:"envelope"`
	State       string                        `json:"state"`
	ResultRef   string                        `json:"result_ref,omitempty"`
	Revision    int64                         `json:"revision"`
}

// AcceptHumanInteraction is only called by a verified SDK callback. A role CLI
// has no endpoint for creating these receipts or supplying identity aliases.
func (t *Tx) AcceptHumanInteraction(raw work.HumanInteractionEnvelope, gateway work.IngressGateway) (v HumanInteraction, replay bool, err error) {
	err = t.atomic(func() error {
		if raw.GatewayRef != gateway.GatewayRef || raw.AppID != gateway.AppID || raw.EventID == "" || raw.Tenant == "" || raw.Kind != "card" || raw.ChatID == "" || raw.MessageID == "" || raw.ReceivedAt.IsZero() {
			return fmt.Errorf("incomplete SDK interaction identity")
		}
		binding, e := t.GetRoleBotBinding(gateway.RoleID)
		if e != nil {
			return e
		}
		if binding.GatewayRef != gateway.GatewayRef || binding.AppID != gateway.AppID || binding.IdentityGeneration != gateway.BotGeneration {
			return ErrConflict
		}
		evidence := work.IdentityEvidence{Tenant: raw.Tenant, AppID: raw.AppID, Generation: gateway.BotGeneration, UnionDomain: gateway.UnionDomain, IDs: raw.SenderIDs, ObservationRef: "sdk-card"}
		aliases := identityAliases(evidence)
		if len(aliases) == 0 {
			return fmt.Errorf("SDK interaction has no stable human identity")
		}
		canonical := raw
		canonical.ReceivedAt = time.Time{}
		body, e := json.Marshal(struct {
			Envelope work.HumanInteractionEnvelope
			Gateway  work.IngressGateway
		}{canonical, gateway})
		if e != nil {
			return e
		}
		if len(body) > 512*1024 {
			return fmt.Errorf("interaction exceeds size limit")
		}
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])
		key := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", gateway.AppID, gateway.BotGeneration, raw.EventID)))
		id := "interaction:" + hex.EncodeToString(key[:])
		var oldHash string
		e = t.tx.QueryRowContext(t.ctx, "SELECT payload_hash FROM human_interactions WHERE id=?", id).Scan(&oldHash)
		if e == nil {
			if oldHash != hash {
				return ErrIdempotencyConflict
			}
			v, e = t.GetHumanInteraction(id)
			replay = true
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		owner := ""
		for _, alias := range aliases {
			if e = alias.Validate(); e != nil {
				return e
			}
			existing, e := t.GetExternalIdentity(alias)
			if e == nil {
				if owner != "" && owner != existing.PrincipalID {
					return ErrIdentityConflict
				}
				owner = existing.PrincipalID
			} else if !errors.Is(e, ErrNotFound) {
				return e
			}
		}
		if owner == "" {
			sum := sha256.Sum256([]byte(mustJSON(aliases[0])))
			owner = "principal:" + hex.EncodeToString(sum[:])
			if e = t.PutPrincipal(work.Principal{ID: owner, DisplayName: aliases[0].ExternalID, Revision: 1}, 0); e != nil {
				return e
			}
		}
		for _, alias := range aliases {
			if _, e = t.tx.ExecContext(t.ctx, "INSERT INTO external_identities(tenant,domain,id_type,external_id,generation,principal_id,evidence_ref) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING", alias.Tenant, alias.AppIdentityDomain, alias.IDType, alias.ExternalID, alias.Generation, owner, id); e != nil {
				return e
			}
		}
		v = HumanInteraction{ID: id, PrincipalID: owner, Gateway: gateway, Envelope: raw, State: "pending", Revision: 1}
		encoded, e := json.Marshal(v)
		if e != nil {
			return e
		}
		_, e = t.tx.ExecContext(t.ctx, "INSERT INTO human_interactions(id,principal_id,state,revision,payload_hash,payload) VALUES(?,?,?,?,?,?)", id, owner, v.State, 1, hash, string(encoded))
		return e
	})
	return
}

func (t *Tx) GetHumanInteraction(id string) (HumanInteraction, error) {
	var v HumanInteraction
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM human_interactions WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

func (t *Tx) ListPendingHumanInteractions(limit int) ([]HumanInteraction, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid interaction limit")
	}
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM human_interactions WHERE state='pending' ORDER BY rowid LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HumanInteraction
	for rows.Next() {
		var raw string
		var v HumanInteraction
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (t *Tx) FinishHumanInteraction(id string, expected int64, resultRef string) error {
	v, err := t.GetHumanInteraction(id)
	if err != nil {
		return err
	}
	if v.Revision != expected {
		return ErrConflict
	}
	if v.State != "pending" {
		if v.ResultRef == resultRef {
			return nil
		}
		return ErrConflict
	}
	v.State = "handled"
	v.ResultRef = resultRef
	v.Revision++
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	result, err := t.tx.ExecContext(t.ctx, "UPDATE human_interactions SET state=?,revision=?,payload=? WHERE id=? AND revision=?", v.State, v.Revision, string(body), id, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

// ListPendingHumanInteractionHeads returns the first durable arrival per chat.
// A blocked chat cannot fill the whole batch and hide other conversations.
func (t *Tx) ListPendingHumanInteractionHeads(limit int) ([]HumanInteraction, error) {
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid interaction limit")
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM (
  SELECT payload,rowid AS arrival,ROW_NUMBER() OVER (
   PARTITION BY json_extract(payload,'$.envelope.tenant'),json_extract(payload,'$.envelope.chat_id') ORDER BY rowid
  ) AS position FROM human_interactions WHERE state='pending'
 ) WHERE position=1 ORDER BY arrival LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []HumanInteraction
	for rows.Next() {
		var raw string
		var value HumanInteraction
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
