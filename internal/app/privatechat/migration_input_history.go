package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type historicalInbox struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	ScopeID   string `json:"scope_id"`
}
type historicalMessage struct {
	ID          string                 `json:"id"`
	ScopeID     string                 `json:"scope_id"`
	BodyRef     string                 `json:"body_ref"`
	Attachments []HistoricalAttachment `json:"attachments"`
}
type HistoricalAttachment struct {
	ContentRef      string `json:"content_ref"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	MediaType       string `json:"media_type"`
	FileName        string `json:"file_name"`
	SourceMessageID string `json:"source_message_id"`
	Kind            string `json:"kind"`
}

// HistoricalInput is a user-requested view of one preserved source input.
// Source identifiers are internal correlation keys and must not be rendered.
type HistoricalInput struct {
	InboxID       string
	ImportID      string
	PayloadHash   string
	Kind          string
	Message       historicalMessage
	Text          string
	TextAvailable bool
	Attachments   []HistoricalInputAttachment
}
type HistoricalInputAttachment struct {
	Attachment HistoricalAttachment
	Path       string
	Available  bool
}

// HistoricalInputs checks the original record and content hashes again under
// the exact owner epoch. Unavailable attachment bytes never become a new input.
func (j *Journal) HistoricalInputs(ctx context.Context, owner Owner, generation int64, limit int) ([]HistoricalInput, error) {
	return j.historicalInputPage(ctx, owner, generation, 0, limit)
}
func (j *Journal) HistoricalInputPage(ctx context.Context, owner Owner, generation int64, page, limit int) ([]HistoricalInput, error) {
	if page < 1 || page > 100000 || limit < 1 || limit > 100 {
		return nil, ErrJournalState
	}
	return j.historicalInputPage(ctx, owner, generation, (page-1)*limit, limit)
}
func (j *Journal) HistoricalInputAt(ctx context.Context, owner Owner, generation int64, index int) (HistoricalInput, bool, error) {
	if index < 1 || index > 1000000 {
		return HistoricalInput{}, false, ErrJournalState
	}
	items, e := j.historicalInputPage(ctx, owner, generation, index-1, 1)
	if e != nil || len(items) == 0 {
		return HistoricalInput{}, false, e
	}
	return items[0], true, nil
}
func (j *Journal) historicalInputPage(ctx context.Context, owner Owner, generation int64, offset, limit int) ([]HistoricalInput, error) {
	if !validOwner(owner) || generation < 1 || offset < 0 || limit < 1 || limit > 100 {
		return nil, ErrJournalState
	}
	var total int
	err := j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_input_holds h JOIN private_migration_imports i ON i.import_id=h.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation).Scan(&total)
	if err != nil {
		return nil, err
	}
	rows, err := j.db.QueryContext(ctx, `SELECT i.import_id,i.source_scope,h.inbox_id,h.message_id,h.payload_hash,r.payload,r.payload_hash,m.payload,m.payload_hash,c.data,c.data_hash,c.payload,c.payload_hash FROM private_migration_input_holds h JOIN private_migration_imports i ON i.import_id=h.import_id LEFT JOIN private_migration_records r ON r.import_id=h.import_id AND r.source_table='inbox' AND r.source_key=h.inbox_id AND r.source_scope=i.source_scope LEFT JOIN private_migration_records m ON m.import_id=h.import_id AND m.source_table='messages' AND m.source_key=h.message_id AND m.source_scope=i.source_scope LEFT JOIN private_migration_records c ON c.import_id=h.import_id AND c.source_table='content' AND c.source_key=json_extract(m.payload,'$.body_ref') AND c.source_scope=i.source_scope WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? ORDER BY r.source_order LIMIT ? OFFSET ?`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoricalInput
	for rows.Next() {
		var importID, scopeID, id, messageID, holdHash, inboxHash, messageHash, contentHash string
		var inboxRaw, messageRaw, body, contentRaw []byte
		var contentPayloadHash string
		if err = rows.Scan(&importID, &scopeID, &id, &messageID, &holdHash, &inboxRaw, &inboxHash, &messageRaw, &messageHash, &body, &contentHash, &contentRaw, &contentPayloadHash); err != nil {
			return nil, err
		}
		if migrationSHA(inboxRaw) != inboxHash || holdHash != inboxHash || migrationSHA(messageRaw) != messageHash || migrationSHA(body) != contentHash || migrationSHA(contentRaw) != contentPayloadHash {
			return nil, ErrJournalConflict
		}
		var inbox historicalInbox
		var m historicalMessage
		if json.Unmarshal(inboxRaw, &inbox) != nil || json.Unmarshal(messageRaw, &m) != nil || inbox.ID != id || inbox.MessageID != messageID || m.ID != messageID || m.ScopeID != inbox.ScopeID || inbox.ScopeID != scopeID {
			return nil, ErrJournalConflict
		}
		var contentMeta struct {
			MediaType string `json:"media_type"`
		}
		if json.Unmarshal(contentRaw, &contentMeta) != nil {
			return nil, ErrJournalConflict
		}
		v := HistoricalInput{InboxID: id, ImportID: importID, PayloadHash: holdHash, Kind: "inbox", Message: m}
		if strings.HasPrefix(contentMeta.MediaType, "text/plain") && utf8.Valid(body) {
			v.Text = string(body)
			v.TextAvailable = true
		}

		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		v := &out[i]
		for _, a := range v.Message.Attachments {
			var path, media, hash string
			var size int64
			e := j.db.QueryRowContext(ctx, `SELECT a.path,a.media_type,a.size,a.sha256 FROM private_migration_assets a JOIN private_migration_imports i ON i.import_id=a.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND a.content_id=? AND a.source_message_id=?`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, a.ContentRef, a.SourceMessageID).Scan(&path, &media, &size, &hash)
			if errors.Is(e, sql.ErrNoRows) {
				v.Attachments = append(v.Attachments, HistoricalInputAttachment{Attachment: a})
				continue
			}
			if e != nil {
				return nil, e
			}
			if media != a.MediaType || size != a.Size || hash != a.SHA256 {
				v.Attachments = append(v.Attachments, HistoricalInputAttachment{Attachment: a})
				continue
			}
			v.Attachments = append(v.Attachments, HistoricalInputAttachment{Attachment: a, Path: path, Available: true})
		}
	}
	if len(out) < limit {
		queueOffset := offset - total
		if queueOffset < 0 {
			queueOffset = 0
		}
		queued, e := j.suspendedHistoricalInputs(ctx, owner, generation, queueOffset, limit-len(out))
		if e != nil {
			return nil, e
		}
		out = append(out, queued...)
	}
	return out, nil
}

// isHistoricalInputReadAction permits only inspection while another migration
// component remains held. A future resubmission must pass normal recovery gates.
func isHistoricalInputReadAction(a control.Action) bool {
	if a.Kind != control.ActionTextMessage {
		return false
	}
	parts := strings.Fields(a.Text)
	if len(parts) == 0 || parts[0] != "/recover" {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	if len(parts) == 3 && parts[1] == "page" {
		n, e := strconv.Atoi(parts[2])
		return e == nil && n > 0 && n <= 100000
	}
	if len(parts) != 2 {
		return false
	}
	n, e := strconv.Atoi(parts[1])
	return e == nil && n > 0 && n <= 1000000
}

type historicalRecoveryRef struct {
	ImportID, InboxID, PayloadHash, Kind string
	Session                              *historicalSessionProof
}

func (j *Journal) commitHistoricalRecovery(ctx context.Context, tx *sql.Tx, c Checkpoint, seq int64, ref historicalRecoveryRef) error {
	if seq < 1 || ref.ImportID == "" || ref.InboxID == "" || ref.PayloadHash == "" {
		return ErrJournalState
	}
	o := c.Owner
	if ref.Session != nil {
		var count int
		e := tx.QueryRowContext(ctx, `SELECT count(*) FROM native_events e JOIN native_commands n ON n.command_id=e.command_id JOIN dispatch d ON d.id=n.dispatch_id WHERE e.incarnation=? AND e.sequence=? AND e.payload_hash=? AND n.payload_hash=? AND d.state='accepted' AND d.event_sequence=e.sequence AND d.carrier=e.incarnation AND d.tenant=? AND d.app_id=? AND d.gateway=? AND d.surface=? AND d.chat=? AND d.user_id=? AND d.generation=? AND json_extract(e.payload,'$.threadId')=?`, ref.Session.Incarnation, ref.Session.Sequence, ref.Session.Hash, ref.Session.CommandHash, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation, ref.Session.SessionID).Scan(&count)
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrJournalConflict
		}
	}
	if ref.Kind == "queue" {
		result, e := tx.ExecContext(ctx, `INSERT INTO private_migration_recovered_inputs(import_id,inbox_id,payload_hash,ingress_seq,revision) SELECT i.import_id,?,q.payload_hash,?,? FROM private_migration_imports i JOIN private_migration_suspended_queue q ON q.import_id=i.import_id JOIN json_each(json_extract(q.payload,'$.QueuedQueueItemIDs')) v ON v.value=? WHERE i.import_id=? AND q.payload_hash=? AND i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?`, "queue:"+ref.InboxID, seq, c.Revision, ref.InboxID, ref.ImportID, ref.PayloadHash, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation)
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
		return persistHistoricalSessionProof(ctx, tx, ref, "queue:"+ref.InboxID)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO private_migration_recovered_inputs(import_id,inbox_id,payload_hash,ingress_seq,revision) SELECT i.import_id,h.inbox_id,h.payload_hash,?,? FROM private_migration_imports i JOIN private_migration_input_holds h ON h.import_id=i.import_id JOIN private_migration_records r ON r.import_id=i.import_id AND r.source_table='inbox' AND r.source_key=h.inbox_id AND r.payload_hash=h.payload_hash WHERE i.import_id=? AND h.inbox_id=? AND h.payload_hash=? AND i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?`, seq, c.Revision, ref.ImportID, ref.InboxID, ref.PayloadHash, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, c.Generation)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrJournalConflict
	}
	return persistHistoricalSessionProof(ctx, tx, ref, ref.InboxID)
}

func persistHistoricalSessionProof(ctx context.Context, tx *sql.Tx, ref historicalRecoveryRef, key string) error {
	if ref.Session == nil {
		return nil
	}
	raw, e := json.Marshal(ref.Session)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO private_migration_recovery_session_proofs(import_id,inbox_id,proof) VALUES(?,?,?)`, ref.ImportID, key, raw)
	return e
}

func (j *Journal) suspendedHistoricalInputs(ctx context.Context, o Owner, generation int64, offset, limit int) ([]HistoricalInput, error) {
	var importID, hash string
	var raw []byte
	e := j.db.QueryRowContext(ctx, `SELECT i.import_id,q.payload,q.payload_hash FROM private_migration_suspended_queue q JOIN private_migration_imports i ON i.import_id=q.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&importID, &raw, &hash)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if migrationSHA(raw) != hash {
		return nil, ErrJournalConflict
	}
	var c orchestrator.PrivateCheckpoint
	if json.Unmarshal(raw, &c) != nil || c.SurfaceID != o.SurfaceID || c.GatewayID != o.GatewayID || c.ChatID != o.ChatID || c.ActorUserID != o.UserID {
		return nil, ErrJournalConflict
	}
	var out []HistoricalInput
	for index, id := range c.QueuedQueueItemIDs {
		if index < offset {
			continue
		}
		if len(out) >= limit {
			break
		}
		item := c.QueueItems[id]
		if item == nil || item.ID != id || item.ActorUserID != o.UserID {
			return nil, ErrJournalConflict
		}
		v := HistoricalInput{Kind: "queue", InboxID: id, ImportID: importID, PayloadHash: hash, TextAvailable: true}
		for _, input := range item.Inputs {
			switch input.Type {
			case agentproto.InputText:
				if v.Text != "" {
					v.Text += "\n"
				}
				v.Text += input.Text
			case agentproto.InputLocalImage:
				var content, source, media, digest string
				var size int64
				e = j.db.QueryRowContext(ctx, `SELECT content_id,source_message_id,media_type,size,sha256 FROM private_migration_assets WHERE import_id=? AND path=?`, importID, input.Path).Scan(&content, &source, &media, &size, &digest)
				if e != nil || media != input.MIMEType {
					return nil, ErrJournalConflict
				}
				v.Attachments = append(v.Attachments, HistoricalInputAttachment{Attachment: HistoricalAttachment{ContentRef: content, SourceMessageID: source, MediaType: media, SHA256: digest, Size: size, Kind: "image"}, Path: input.Path, Available: true})
			default:
				return nil, ErrJournalConflict
			}
		}
		out = append(out, v)
	}
	return out, nil
}
