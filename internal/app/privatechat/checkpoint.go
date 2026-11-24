package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type Effect struct {
	Lane       string
	ID         string
	Body       json.RawMessage
	DisplayKey string `json:",omitempty"`
}
type Checkpoint struct {
	Owner                        Owner
	Generation, Revision, Cursor int64
	Core                         orchestrator.PrivateCheckpoint
}
type EffectRecord struct {
	Effect   Effect
	Revision int64
	Ordinal  int
	State    string
	Receipt  json.RawMessage
}

// CommitCheckpoint atomically stores the core projection, applied ingress
// watermark, and ordered effects. The caller must serially apply only admitted
// events up to cursor; no effect may run before this method commits.
func (j *Journal) CommitCheckpoint(ctx context.Context, c Checkpoint, expected int64, effects []Effect) error {
	return j.commitCheckpoint(ctx, c, expected, effects, 0, nil)
}

func (j *Journal) commitCheckpoint(ctx context.Context, c Checkpoint, expected int64, effects []Effect, appliedSeq int64, nativeRef *NativeEventRef) error {
	return j.commitCheckpointExtended(ctx, c, expected, effects, appliedSeq, nativeRef, nil, nil, nil, nil, nil, nil)
}

func (j *Journal) commitCheckpointExtended(ctx context.Context, c Checkpoint, expected int64, effects []Effect, appliedSeq int64, nativeRef *NativeEventRef, card *CardResponse, host *hostEffectRef, live *liveEffectRef, settings *settingsReconcileRef, disconnect *disconnectRef, tick *tickRef, migration ...*migrationResumeRef) error {
	if len(migration) > 1 {
		return ErrJournalState
	}
	var resume *migrationResumeRef
	if len(migration) == 1 {
		resume = migration[0]
	}
	if resume != nil && resume.Historical == nil && (card != nil || host != nil || live != nil || settings != nil || disconnect != nil || tick != nil || appliedSeq != 0 || nativeRef != nil) {
		return ErrJournalState
	}
	o := c.Owner
	if !validOwner(o) || c.Generation < 1 || expected < 0 || c.Revision != expected+1 || c.Cursor < 0 || (c.Core.Version != 1 && c.Core.Version != 2) || (c.Core.Version == 1 && (len(c.Core.DetachedExecutions) != 0 || c.Core.OfflineStop != nil || c.Core.ReconnectQuery != nil || c.Core.Goal != nil || c.Core.Navigation != nil)) || c.Core.SurfaceID != o.SurfaceID || c.Core.GatewayID != o.GatewayID || c.Core.ChatID != o.ChatID || c.Core.ActorUserID != o.UserID {
		return ErrJournalState
	}
	validator := orchestrator.NewService(nil, orchestrator.Config{PrivateApplication: true}, nil)
	if err := validator.RestorePrivateCheckpoint(c.Core); err != nil {
		return fmt.Errorf("invalid core checkpoint: %w", err)
	}
	effects = append([]Effect(nil), effects...)
	for i := range effects {
		if effects[i].Lane != "interactive" && effects[i].Lane != "background" {
			return ErrJournalState
		}
	}
	seen := map[string]bool{}
	for _, e := range effects {
		if e.ID == "" || seen[e.ID] || !json.Valid(e.Body) {
			return ErrJournalState
		}
		seen[e.ID] = true
	}
	raw, err := json.Marshal(c.Core)
	if err != nil {
		return err
	}
	base := struct {
		Cursor     int64
		AppliedSeq int64
		NativeRef  *NativeEventRef
		Core       json.RawMessage
		Effects    []Effect
	}{c.Cursor, appliedSeq, nativeRef, raw, effects}
	var identity []byte
	if card == nil && host == nil && live == nil && settings == nil && disconnect == nil && tick == nil && resume == nil {
		identity, err = json.Marshal(base)
	} else if resume == nil {
		identity, err = json.Marshal(struct {
			Base       any
			Card       *CardResponse
			Host       *hostEffectRef
			Live       *liveEffectRef
			Settings   *settingsReconcileRef
			Disconnect *disconnectRef
			Tick       *tickRef
		}{base, card, host, live, settings, disconnect, tick})
	} else {
		identity, err = json.Marshal(struct {
			Base   any
			Resume *migrationResumeRef
		}{base, resume})
	}
	if err != nil {
		return err
	}
	hash := sha256.Sum256(identity)
	digest := hex.EncodeToString(hash[:])
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision, cursor int64
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT revision,cursor,digest FROM private_checkpoint WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation).Scan(&revision, &cursor, &previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && revision == c.Revision {
		if previous == digest {
			return nil
		}
		return ErrJournalConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		revision = 0
		cursor = 0
	}
	if revision != expected || c.Cursor < cursor {
		return ErrJournalConflict
	}
	if c.Cursor > cursor {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM ingress WHERE seq=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, c.Cursor, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("checkpoint cursor is not owned ingress: %w", ErrJournalState)
		}
	}
	if err = verifyCheckpointSources(ctx, tx, c); err != nil {
		return err
	}
	if appliedSeq != 0 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM ingress WHERE seq=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, appliedSeq, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrJournalState
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_applied(seq,revision) VALUES(?,?)`, appliedSeq, c.Revision); err != nil {
			return err
		}
		if card != nil {
			if err := validateCardResponse(*card); err != nil {
				return err
			}
			body, e := json.Marshal(card)
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO private_callback_response(seq,revision,body) VALUES(?,?,?)`, appliedSeq, c.Revision, body); e != nil {
				return e
			}
		}
	} else if card != nil {
		return ErrJournalState
	}
	if host != nil {
		if appliedSeq != 0 || nativeRef != nil {
			return ErrJournalState
		}
		var receipt []byte
		if err = tx.QueryRowContext(ctx, `SELECT receipt FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state='confirmed'`, host.ID, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation).Scan(&receipt); err != nil {
			return err
		}
		h := sha256.Sum256(receipt)
		if hex.EncodeToString(h[:]) != host.ReceiptDigest {
			return ErrJournalConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_effect_applied(effect_id,revision,receipt_digest) VALUES(?,?,?)`, host.ID, c.Revision, host.ReceiptDigest); err != nil {
			return err
		}
		if host.SettingsVersion != "" {
			_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO private_settings_reconcile(version,tenant,app_id,gateway,surface,chat,user_id,generation) SELECT ?,p.tenant,p.app_id,p.gateway,p.surface,p.chat,p.user_id,p.generation FROM private_checkpoint p WHERE p.tenant=? AND p.app_id=? AND p.gateway=? AND p.surface<>? AND p.generation=(SELECT max(q.generation) FROM private_checkpoint q WHERE q.tenant=p.tenant AND q.app_id=p.app_id AND q.gateway=p.gateway AND q.surface=p.surface AND q.chat=p.chat AND q.user_id=p.user_id)`, host.SettingsVersion, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID)
			if err != nil {
				return err
			}
		}
	}
	if live != nil {
		if appliedSeq != 0 || nativeRef != nil || host != nil || card != nil || live.Incarnation == "" || live.Sequence < 1 || live.Hash == "" {
			return ErrJournalState
		}
		var latest sql.NullInt64
		if err = tx.QueryRowContext(ctx, `SELECT max(sequence) FROM private_live_applied WHERE incarnation=?`, live.Incarnation).Scan(&latest); err != nil {
			return err
		}
		if latest.Valid && live.Sequence <= latest.Int64 {
			return ErrJournalConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_live_applied(incarnation,sequence,payload_hash,revision) VALUES(?,?,?,?)`, live.Incarnation, live.Sequence, live.Hash, c.Revision); err != nil {
			return err
		}
	}
	if settings != nil {
		if appliedSeq != 0 || nativeRef != nil || host != nil || live != nil || card != nil || settings.Version == "" {
			return ErrJournalState
		}
		result, e := tx.ExecContext(ctx, `UPDATE private_settings_reconcile SET state='applied',applied_revision=? WHERE version=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state='pending'`, c.Revision, settings.Version, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrJournalConflict
		}
	}
	if disconnect != nil {
		if appliedSeq != 0 || nativeRef != nil || host != nil || live != nil || settings != nil || card != nil || disconnect.Incarnation == "" || disconnect.ConnectionID == "" {
			return ErrJournalState
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_disconnect_applied(incarnation,connection_id,revision) VALUES(?,?,?)`, disconnect.Incarnation, disconnect.ConnectionID, c.Revision); err != nil {
			return err
		}
	}
	if tick != nil {
		if appliedSeq != 0 || nativeRef != nil || host != nil || live != nil || settings != nil || disconnect != nil || card != nil || tick.ID == "" || tick.At == "" {
			return ErrJournalState
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_tick_applied(tenant,app_id,gateway,surface,chat,user_id,generation,tick_id,observed_at,revision) VALUES(?,?,?,?,?,?,?,?,?,?)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation, tick.ID, tick.At, c.Revision); err != nil {
			return err
		}
	}
	if nativeRef != nil {
		if appliedSeq != 0 || nativeRef.Incarnation == "" || nativeRef.Sequence == 0 || nativeRef.PayloadHash == "" {
			return ErrJournalState
		}
		var earlier int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM native_events WHERE incarnation=? AND sequence<? AND applied=0`, nativeRef.Incarnation, nativeRef.Sequence).Scan(&earlier); err != nil {
			return err
		}
		if earlier != 0 {
			return ErrJournalState
		}
		if nativeRef.Sequence > 1 {
			var previous int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM native_events WHERE incarnation=? AND sequence=? AND applied=1`, nativeRef.Incarnation, nativeRef.Sequence-1).Scan(&previous); err != nil {
				return err
			}
			if previous != 1 {
				return ErrJournalState
			}
		}
		result, e := tx.ExecContext(ctx, `UPDATE native_events SET applied=1,applied_revision=? WHERE incarnation=? AND sequence=? AND payload_hash=? AND applied=0`, c.Revision, nativeRef.Incarnation, nativeRef.Sequence, nativeRef.PayloadHash)
		if e != nil {
			return e
		}
		count, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrJournalConflict
		}
	}
	if expected == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO private_checkpoint(tenant,app_id,gateway,surface,chat,user_id,generation,revision,cursor,data,digest) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation, c.Revision, c.Cursor, raw, digest)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE private_checkpoint SET revision=?,cursor=?,data=?,digest=? WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND revision=?`, c.Revision, c.Cursor, raw, digest, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation, expected)
	}
	if err != nil {
		return err
	}
	for i, e := range effects {
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'prepared')`, e.ID, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation, c.Revision, i, e.Lane, []byte(e.Body)); err != nil {
			return err
		}
		if e.DisplayKey != "" {
			if _, err = tx.ExecContext(ctx, `INSERT INTO private_display_chain(effect_id,chain_key) VALUES(?,?)`, e.ID, e.DisplayKey); err != nil {
				return err
			}
		}
	}
	if resume != nil && resume.Historical != nil {
		if err = j.commitHistoricalRecovery(ctx, tx, c, appliedSeq, *resume.Historical); err != nil {
			return err
		}
	} else if resume != nil && resume.Connection != nil {
		if err = j.commitNativeConnection(ctx, tx, c, *resume.Connection); err != nil {
			return err
		}
	} else if resume != nil && resume.Upgrade != nil {
		if !resume.Upgrade.Pause && resume.Upgrade.NewInstanceID == "" && resume.Upgrade.HelloIncarnation == "" && !seen[resume.Upgrade.EffectID] {
			return ErrJournalConflict
		}
		if err = j.commitUpgradeRecovery(ctx, tx, c, *resume.Upgrade); err != nil {
			return err
		}
	} else if resume != nil {
		if resume.ImportID == "" || resume.EffectID == "" || resume.PlanHash == "" || !seen[resume.EffectID] {
			return ErrJournalState
		}
		result, e := tx.ExecContext(ctx, `UPDATE private_migration_resume SET effect_id=? WHERE import_id=? AND plan_hash=? AND effect_id='' AND EXISTS (SELECT 1 FROM private_migration_imports i WHERE i.import_id=private_migration_resume.import_id AND i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?)`, resume.EffectID, resume.ImportID, resume.PlanHash, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrJournalConflict
		}
	}
	// Version 2 detached evidence must fence older writers before the same
	// transaction makes the checkpoint visible, including background senders.
	if c.Core.Version == 2 {
		var schemaVersion int
		if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&schemaVersion); err != nil {
			return err
		}
		if schemaVersion < 6 {
			if _, err = tx.ExecContext(ctx, `PRAGMA user_version=6`); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (j *Journal) LoadCheckpoint(ctx context.Context, o Owner, generation int64) (Checkpoint, error) {
	c := Checkpoint{Owner: o, Generation: generation}
	if !validOwner(o) || generation < 1 {
		return c, ErrJournalState
	}
	var raw []byte
	err := j.db.QueryRowContext(ctx, `SELECT revision,cursor,data FROM private_checkpoint WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&c.Revision, &c.Cursor, &raw)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(raw, &c.Core)
	return c, err
}

// PreparedEffects exposes only committed effects; an uncertain send is never
// returned as prepared. Ordered pages use revision/ordinal as a stable cursor.
func (j *Journal) PreparedEffects(ctx context.Context, o Owner, generation, afterRevision int64, afterOrdinal, limit int) ([]EffectRecord, error) {
	if !validOwner(o) || generation < 1 || afterRevision < 0 || afterOrdinal < -1 || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT id,revision,ordinal,lane,body,state,receipt FROM private_effect WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state='prepared' AND (revision>? OR (revision=? AND ordinal>?)) ORDER BY revision,ordinal LIMIT ?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, afterRevision, afterRevision, afterOrdinal, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EffectRecord
	for rows.Next() {
		var e EffectRecord
		var receipt []byte
		if err = rows.Scan(&e.Effect.ID, &e.Revision, &e.Ordinal, &e.Effect.Lane, &e.Effect.Body, &e.State, &receipt); err != nil {
			return nil, err
		}
		e.Receipt = receipt
		out = append(out, e)
	}
	return out, rows.Err()
}
func (j *Journal) MarkEffectSending(ctx context.Context, o Owner, generation int64, id string) error {
	if !validOwner(o) || generation < 1 {
		return ErrJournalState
	}
	res, err := j.db.ExecContext(ctx, `UPDATE private_effect SET state='sending' WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state='prepared' AND NOT EXISTS (SELECT 1 FROM private_display_chain current JOIN private_display_chain pc ON pc.chain_key=current.chain_key JOIN private_effect prior ON prior.id=pc.effect_id LEFT JOIN private_effect_applied applied ON applied.effect_id=prior.id WHERE current.effect_id=private_effect.id AND prior.tenant=private_effect.tenant AND prior.app_id=private_effect.app_id AND prior.gateway=private_effect.gateway AND prior.surface=private_effect.surface AND prior.chat=private_effect.chat AND prior.user_id=private_effect.user_id AND prior.generation=private_effect.generation AND (prior.revision<private_effect.revision OR (prior.revision=private_effect.revision AND prior.ordinal<private_effect.ordinal)) AND (prior.state<>'confirmed' OR applied.effect_id IS NULL))`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrJournalState
	}
	return nil
}

// RecoveryEffects includes sending/unknown records for reconciliation and
// never treats their absence from PreparedEffects as proof of delivery.
func (j *Journal) RecoveryEffects(ctx context.Context, o Owner, generation, afterRevision int64, afterOrdinal, limit int) ([]EffectRecord, error) {
	if !validOwner(o) || generation < 1 || afterRevision < 0 || afterOrdinal < -1 || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT id,revision,ordinal,lane,body,state,receipt FROM private_effect WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND (revision>? OR (revision=? AND ordinal>?)) ORDER BY revision,ordinal LIMIT ?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, afterRevision, afterRevision, afterOrdinal, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EffectRecord
	for rows.Next() {
		var e EffectRecord
		var receipt []byte
		if err = rows.Scan(&e.Effect.ID, &e.Revision, &e.Ordinal, &e.Effect.Lane, &e.Effect.Body, &e.State, &receipt); err != nil {
			return nil, err
		}
		e.Receipt = receipt
		out = append(out, e)
	}
	return out, rows.Err()
}

// EffectReceiptVerifier must establish the platform message identity or other
// external completion evidence; a transport write alone is insufficient.
type EffectReceiptVerifier interface {
	VerifyEffect(context.Context, Owner, int64, Effect, json.RawMessage) error
}

func (j *Journal) ResolveEffect(ctx context.Context, o Owner, generation int64, id, outcome string, receipt json.RawMessage, verifier EffectReceiptVerifier) error {
	if !validOwner(o) || generation < 1 || id == "" || (outcome != "confirmed" && outcome != "unknown") || !json.Valid(receipt) || string(receipt) == "null" {
		return ErrJournalState
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var effect Effect
	effect.ID = id
	var state string
	var saved []byte
	err = tx.QueryRowContext(ctx, `SELECT lane,body,state,receipt FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&effect.Lane, &effect.Body, &state, &saved)
	if err != nil {
		return err
	}
	if state == outcome && string(saved) == string(receipt) {
		return nil
	}
	if state != "sending" && !(state == "unknown" && outcome == "confirmed") {
		return ErrJournalState
	}
	if outcome == "confirmed" {
		if verifier == nil {
			return ErrJournalState
		}
		if err = verifier.VerifyEffect(ctx, o, generation, effect, receipt); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE private_effect SET state=?,receipt=? WHERE id=?`, outcome, []byte(receipt), id); err != nil {
		return err
	}
	return tx.Commit()
}

// Source IDs in queued and staged records must come from committed ingress;
// the checkpoint cannot create its own evidence of user input.
func verifyCheckpointSources(ctx context.Context, tx *sql.Tx, c Checkpoint) error {
	required := map[string]bool{}
	add := func(id string) {
		if id != "" {
			required[id] = true
		}
	}
	for _, item := range c.Core.QueueItems {
		if item == nil {
			return ErrJournalState
		}
		add(item.SourceMessageID)
		for _, id := range item.SourceMessageIDs {
			add(id)
		}
	}
	for _, image := range c.Core.StagedImages {
		if image == nil {
			return ErrJournalState
		}
		add(image.SourceMessageID)
	}
	for _, file := range c.Core.StagedFiles {
		if file == nil {
			return ErrJournalState
		}
		add(file.SourceMessageID)
	}
	if len(required) == 0 {
		return nil
	}
	o := c.Owner
	rows, err := tx.QueryContext(ctx, `SELECT sources FROM ingress WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND seq<=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation, c.Cursor)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		var sources []string
		if err = json.Unmarshal(raw, &sources); err != nil {
			return err
		}
		for _, id := range sources {
			delete(required, id)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(required) > 0 {
		return fmt.Errorf("checkpoint references input without committed ingress: %w", ErrJournalState)
	}
	return nil
}
