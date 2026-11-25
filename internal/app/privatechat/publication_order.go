package privatechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func publicationOrderKeys(o Owner, generation int64, scopes []string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, scope := range scopes {
		if scope == "" {
			continue
		}
		raw, _ := json.Marshal(struct {
			Owner      Owner
			Generation int64
			Scope      string
		}{o, generation, scope})
		sum := sha256.Sum256(raw)
		key := hex.EncodeToString(sum[:])
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	return keys
}

// Visit only the current effect's indexed dependencies before loading prior
// effect rows. The owner/revision index would otherwise scan unrelated history.
const publicationOrderHeldSQL = `EXISTS(SELECT 1 FROM private_publication_order current
CROSS JOIN private_publication_order prior ON prior.order_key=current.order_key
CROSS JOIN private_effect p ON p.id=prior.effect_id
WHERE current.effect_id=e.id AND p.tenant=e.tenant AND p.app_id=e.app_id AND p.gateway=e.gateway AND p.surface=e.surface AND p.chat=e.chat AND p.user_id=e.user_id AND p.generation=e.generation
AND (p.revision<e.revision OR (p.revision=e.revision AND p.ordinal<e.ordinal))
AND NOT EXISTS(SELECT 1 FROM private_effect_applied a WHERE a.effect_id=p.id)
AND NOT (coalesce(CASE WHEN json_valid(p.body) THEN json_extract(p.body,'$.event.TimelineText.Type') END,'')='recovery_input_waiting'
	AND p.state IN ('unknown','confirmed')))`

// PublicationOrderReady is a causal barrier, independent of replacement-card
// receipt resolution. Only publications sharing an original operation wait.
// Unknown sends remain evidence; independent operations can continue.
func (j *Journal) PublicationOrderReady(ctx context.Context, o Owner, generation int64, id string) error {
	if !validOwner(o) || generation < 1 || id == "" {
		return ErrJournalState
	}
	var n int
	err := j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e WHERE e.id=? AND e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND e.lane='interactive'`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrJournalConflict
	}
	err = j.db.QueryRowContext(ctx, `SELECT `+publicationOrderHeldSQL+` FROM private_effect e WHERE e.id=?`, id).Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return ErrDisplayHeld
	}
	return nil
}

// Every visible consequence of a turn shares the turn's ordering scope,
// including summaries which intentionally do not repeat IDs in their DTO.
func bindNativePublicationOrder(owner Owner, generation int64, carrier, thread, turn string, events []eventcontract.Event, effects []Effect) {
	if carrier == "" || thread == "" || turn == "" {
		return
	}
	keys := publicationOrderKeys(owner, generation, []string{"native-turn:" + carrier + "\x00" + thread + "\x00" + turn})
	for i := range effects {
		if effects[i].Lane == "interactive" && (publicationEpisode(events[i]) == "content" || events[i].Notice != nil) {
			effects[i].OrderKeys = append(effects[i].OrderKeys, keys...)
		}
	}
}

// Decorations and tool cards have independent receipt lifecycles. An unknown
// emoji update must not prevent the user's answer from being delivered.
func publicationEpisode(event eventcontract.Event) string {
	if event.PendingInput != nil {
		return "reaction"
	}
	if event.Block != nil || event.ImageOutput != nil || event.FinalTurnSummary != nil {
		return "content"
	}
	if event.ExecCommandProgress != nil {
		return "progress"
	}
	return "navigation"
}

// HasUnsettledCardWrite is read under the Processor's state lock before deciding
// whether a callback can replace its card synchronously. It does not reject the
// action: a contested replacement is published through the existing outbox.
func (j *Journal) HasUnsettledCardWrite(ctx context.Context, owner Owner, generation int64, message string) (bool, error) {
	if !validOwner(owner) || generation < 1 || message == "" {
		return false, ErrJournalState
	}
	return j.hasEarlierCardWrite(ctx, owner, generation, "", message)
}

// CardPublicationOrderReady also covers effects frozen before a physical card
// ID existed. A confirmed predecessor in their original display chain supplies
// that identity; current surface state is never used.
func (j *Journal) CardPublicationOrderReady(ctx context.Context, owner Owner, generation int64, id, message string) error {
	if !validOwner(owner) || generation < 1 || id == "" || message == "" {
		return ErrJournalState
	}
	if err := j.PublicationOrderReady(ctx, owner, generation, id); err != nil {
		return err
	}
	pending, err := j.hasEarlierCardWrite(ctx, owner, generation, id, message)
	if err != nil {
		return err
	}
	if pending {
		return ErrDisplayHeld
	}
	return nil
}

func (j *Journal) hasEarlierCardWrite(ctx context.Context, owner Owner, generation int64, id, message string) (bool, error) {
	key := publicationOrderKeys(owner, generation, []string{"message:" + message})[0]
	var pending bool
	err := j.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM private_effect e
 WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND e.lane='interactive'
 AND NOT EXISTS(SELECT 1 FROM private_effect_applied a WHERE a.effect_id=e.id)
 AND (?='' OR EXISTS(SELECT 1 FROM private_effect current WHERE current.id=? AND (e.revision<current.revision OR (e.revision=current.revision AND e.ordinal<current.ordinal))))
 AND (EXISTS(SELECT 1 FROM private_publication_order c WHERE c.effect_id=e.id AND c.order_key=?)
 OR EXISTS(SELECT 1 FROM private_publication_ops p WHERE p.effect_id=e.id AND json_extract(p.body,'$.Operation.Kind')='update_card' AND json_extract(p.body,'$.Operation.MessageID')=?)
 OR EXISTS(SELECT 1 FROM private_publication_ops p WHERE p.effect_id=e.id AND p.state='confirmed' AND json_valid(p.receipt) AND json_extract(p.receipt,'$.Kind') IN ('send_card','update_card') AND json_extract(p.receipt,'$.MessageID')=?)
 OR EXISTS(SELECT 1 FROM private_display_chain own JOIN private_display_chain previous ON previous.chain_key=own.chain_key
 JOIN private_effect p ON p.id=previous.effect_id JOIN private_effect_applied a ON a.effect_id=p.id
 JOIN json_each(CASE WHEN json_valid(p.receipt) THEN json_extract(p.receipt,'$.Results') ELSE '[]' END) result
 WHERE own.effect_id=e.id AND p.tenant=e.tenant AND p.app_id=e.app_id AND p.gateway=e.gateway AND p.surface=e.surface AND p.chat=e.chat AND p.user_id=e.user_id AND p.generation=e.generation
 AND (p.revision<e.revision OR (p.revision=e.revision AND p.ordinal<e.ordinal)) AND p.state='confirmed'
 AND json_extract(result.value,'$.Receipt.Kind') IN ('send_card','update_card') AND json_extract(result.value,'$.Receipt.MessageID')=?)))`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, id, id, key, message, message, message).Scan(&pending)
	return pending, err
}
