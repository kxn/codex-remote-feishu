package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

var ErrDisplayHeld = errors.New("private display predecessor requires confirmed delivery")

func bindNativeDisplayChains(o Owner, carrier string, events []eventcontract.Event, effects []Effect) error {
	if len(events) != len(effects) || carrier == "" {
		return ErrJournalState
	}
	for i, event := range events {
		progress := event.Normalized().ExecCommandProgress
		if progress == nil {
			continue
		}
		if progress.ThreadID == "" || progress.TurnID == "" || event.SurfaceSessionID != o.SurfaceID {
			return fmt.Errorf("progress lacks exact display identity")
		}
		h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s", o.TenantID, o.AppID, o.SurfaceID, carrier, progress.ThreadID, progress.TurnID)))
		effects[i].DisplayKey = hex.EncodeToString(h[:])
	}
	return nil
}

func explicitCardDisplayKey(o Owner, event eventcontract.Event) string {
	event = event.Normalized()
	if event.PendingInput != nil && event.PendingInput.SourceMessageID != "" {
		h := sha256.Sum256([]byte(fmt.Sprintf("reaction\x00%s\x00%s\x00%s\x00%s", o.TenantID, o.AppID, o.SurfaceID, event.PendingInput.SourceMessageID)))
		return hex.EncodeToString(h[:])
	}
	var id string
	id = event.Meta.MessageDelivery.TargetMessageID
	switch {
	case id != "":
	case event.PageView != nil:
		id = event.PageView.MessageID
	case event.PathPickerView != nil:
		id = event.PathPickerView.MessageID
	case event.TargetPickerView != nil:
		id = event.TargetPickerView.MessageID
	case event.ThreadHistoryView != nil:
		id = event.ThreadHistoryView.MessageID
	case event.RequestView != nil:
		id = event.RequestView.MessageID
	}
	if id == "" && event.InlineReplaceCurrentCard {
		id = event.SourceMessageID
	}
	if id == "" {
		return ""
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("card\x00%s\x00%s\x00%s\x00%s", o.TenantID, o.AppID, o.SurfaceID, id)))
	return hex.EncodeToString(h[:])
}

// ResolveDisplayPredecessor returns a verified, applied predecessor receipt.
// A nil receipt means this is the first event of an independent display chain.
// The caller may use it to enrich only presentation; the stored event is fixed.
func (j *Journal) ResolveDisplayPredecessor(ctx context.Context, o Owner, generation int64, id string) (*PublicationReceipt, error) {
	if !validOwner(o) || generation < 1 || id == "" {
		return nil, ErrJournalState
	}
	var currentKey sql.NullString
	var rev int64
	var ordinal int
	err := j.db.QueryRowContext(ctx, `SELECT c.chain_key,e.revision,e.ordinal FROM private_effect e LEFT JOIN private_display_chain c ON c.effect_id=e.id WHERE e.id=? AND e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&currentKey, &rev, &ordinal)
	if err != nil {
		return nil, err
	}
	if !currentKey.Valid {
		return nil, nil
	}
	var state string
	var raw []byte
	var applied sql.NullInt64
	err = j.db.QueryRowContext(ctx, `SELECT p.state,p.receipt,a.revision FROM private_display_chain c JOIN private_effect p ON p.id=c.effect_id LEFT JOIN private_effect_applied a ON a.effect_id=p.id WHERE c.chain_key=? AND p.tenant=? AND p.app_id=? AND p.gateway=? AND p.surface=? AND p.chat=? AND p.user_id=? AND p.generation=? AND (p.revision<? OR (p.revision=? AND p.ordinal<?)) ORDER BY p.revision DESC,p.ordinal DESC LIMIT 1`, currentKey.String, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, rev, rev, ordinal).Scan(&state, &raw, &applied)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if state != "confirmed" || !applied.Valid {
		return nil, ErrDisplayHeld
	}
	var receipt PublicationReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

type DisplayReceipt struct {
	Revision int64
	Ordinal  int
	Receipt  PublicationReceipt
}

// ListDisplayHistory reads confirmed and applied predecessors newest first.
// Use the returned revision/ordinal cursor for bounded reaction reconciliation.
func (j *Journal) ListDisplayHistory(ctx context.Context, o Owner, generation int64, id string, beforeRevision int64, beforeOrdinal, limit int) ([]DisplayReceipt, error) {
	if !validOwner(o) || generation < 1 || id == "" || limit < 1 || limit > 64 || beforeRevision < 0 {
		return nil, ErrJournalState
	}
	if _, err := j.ResolveDisplayPredecessor(ctx, o, generation, id); err != nil {
		return nil, err
	}
	var key sql.NullString
	var revision int64
	var ordinal int
	err := j.db.QueryRowContext(ctx, `SELECT c.chain_key,e.revision,e.ordinal FROM private_effect e LEFT JOIN private_display_chain c ON c.effect_id=e.id WHERE e.id=? AND e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&key, &revision, &ordinal)
	if err != nil {
		return nil, err
	}
	if !key.Valid {
		return nil, nil
	}
	if beforeRevision > 0 && (beforeRevision < revision || beforeRevision == revision && beforeOrdinal < ordinal) {
		revision, ordinal = beforeRevision, beforeOrdinal
	}
	rows, err := j.db.QueryContext(ctx, `SELECT p.revision,p.ordinal,p.state,p.receipt,a.revision FROM private_display_chain c JOIN private_effect p ON p.id=c.effect_id LEFT JOIN private_effect_applied a ON a.effect_id=p.id WHERE c.chain_key=? AND p.tenant=? AND p.app_id=? AND p.gateway=? AND p.surface=? AND p.chat=? AND p.user_id=? AND p.generation=? AND (p.revision<? OR (p.revision=? AND p.ordinal<?)) ORDER BY p.revision DESC,p.ordinal DESC LIMIT ?`, key.String, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, revision, revision, ordinal, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DisplayReceipt
	for rows.Next() {
		var item DisplayReceipt
		var state string
		var raw []byte
		var applied sql.NullInt64
		if err = rows.Scan(&item.Revision, &item.Ordinal, &state, &raw, &applied); err != nil {
			return nil, err
		}
		if state != "confirmed" || !applied.Valid {
			return nil, ErrDisplayHeld
		}
		if err = json.Unmarshal(raw, &item.Receipt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
