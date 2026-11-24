package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type ReconnectStageVerifier interface {
	VerifyReconnectStage(control.Action, Effect, json.RawMessage, *state.PendingTextInputRecord) (bool, error)
}

// reconnectPendingInputProven proves this is a previously applied SDK input
// which has never acquired a native dispatch, not a migrated claimed input.
func (p *Processor) reconnectPendingInputProven(ctx context.Context, o Owner, generation int64, v *state.PendingTextInputRecord) (bool, error) {
	if v == nil {
		return true, nil
	}
	if v.SourceMessageID == "" || v.ActorUserID != o.UserID {
		return false, nil
	}
	ids := append([]string{v.SourceMessageID}, v.StagedMessageIDs...)
	seen := map[string]bool{}
	for index, id := range ids {
		if id == "" || seen[id] {
			return false, nil
		}
		seen[id] = true
		rows, err := p.journal.db.QueryContext(ctx, `SELECT i.seq FROM ingress i JOIN private_applied a ON a.seq=i.seq AND a.canceled_by IS NULL JOIN json_each(i.sources) src WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND i.lane='input' AND src.value=? AND NOT EXISTS(SELECT 1 FROM dispatch_input d WHERE d.seq=i.seq) AND NOT EXISTS(SELECT 1 FROM private_migration_input_holds h JOIN private_migration_imports m ON m.import_id=h.import_id WHERE m.tenant=i.tenant AND m.app_id=i.app_id AND m.gateway=i.gateway AND m.surface=i.surface AND m.chat=i.chat AND m.user_id=i.user_id AND m.generation=i.generation AND h.message_id=src.value)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, id)
		if err != nil {
			return false, err
		}
		var seq int64
		count := 0
		for rows.Next() {
			if err = rows.Scan(&seq); err != nil {
				rows.Close()
				return false, err
			}
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		if count != 1 {
			return false, nil
		}
		if index == 0 {
			entry, applied, revision, err := p.journal.processorIngress(ctx, o, generation, seq)
			if err != nil {
				return false, err
			}
			if !applied {
				return false, nil
			}
			action, err := p.decoder.Decode(entry)
			if err != nil {
				return false, err
			}
			if action.Kind != control.ActionTextMessage || action.MessageID != v.SourceMessageID || action.ActorUserID != v.ActorUserID || strings.TrimSpace(action.Text) != v.Text || (v.ReplyToMessageID != "" && v.ReplyToMessageID != action.MessageID) {
				return false, nil
			}
			if !reflect.DeepEqual(action.Inputs, v.Inputs) {
				verifier, ok := p.reducer.(ReconnectStageVerifier)
				if !ok {
					return false, nil
				}
				effects, err := p.journal.processorEffects(ctx, o, generation, revision)
				if err != nil {
					return false, err
				}
				proven := false
				for _, effect := range effects {
					if effect.State != "confirmed" {
						continue
					}
					var digest string
					err := p.journal.db.QueryRowContext(ctx, `SELECT receipt_digest FROM private_effect_applied WHERE effect_id=?`, effect.Effect.ID).Scan(&digest)
					if errors.Is(err, sql.ErrNoRows) {
						continue
					}
					if err != nil {
						return false, err
					}
					hash := sha256.Sum256(effect.Receipt)
					if hex.EncodeToString(hash[:]) != digest {
						return false, ErrJournalConflict
					}
					matched, err := verifier.VerifyReconnectStage(action, effect.Effect, effect.Receipt, v)
					if err != nil {
						return false, err
					}
					if matched {
						if proven {
							return false, nil
						}
						proven = true
					}
				}
				if !proven {
					return false, nil
				}
			}
		}
	}
	return true, nil
}
