package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPrivateProjectShareV1 = `CREATE TABLE IF NOT EXISTS private_project_shares(id TEXT PRIMARY KEY,project_id TEXT NOT NULL,scope_id TEXT NOT NULL,principal_id TEXT NOT NULL,source_role_id TEXT NOT NULL,message_id TEXT NOT NULL UNIQUE,inbox_id TEXT NOT NULL UNIQUE,source_hash TEXT NOT NULL,payload BLOB NOT NULL,payload_hash TEXT NOT NULL,created_at TEXT NOT NULL);`

type PrivateProjectShare struct {
	ID, ProjectID, PrincipalID, SourceRoleID, PrivateOwnerID string
	ResourceID, VersionID, ResourceHash                      string
	PrivateEpoch, SourceBotGeneration                        int64
	DraftHash, ConsentEventID, ConsentFactHash, Text         string
	CreatedAt                                                time.Time
}
type PrivateProjectShareReceipt struct{ ID, MessageID, InboxID, ProjectID, ContentHash string }

// AdmitPrivateProjectShare is called only by the authenticated private host
// after validating the person's exact card confirmation and frozen draft.
// The record and leader inbox are committed together, with no team worker token.
func (t *Tx) AdmitPrivateProjectShare(v PrivateProjectShare) (PrivateProjectShareReceipt, error) {
	var receipt PrivateProjectShareReceipt
	if v.ID == "" || v.ProjectID == "" || v.PrincipalID == "" || v.SourceRoleID == "" || v.PrivateOwnerID == "" || v.PrivateEpoch < 1 || v.SourceBotGeneration < 1 || v.DraftHash == "" || v.ConsentEventID == "" || v.ConsentFactHash == "" || v.Text == "" || len(v.Text) > 64<<10 || v.CreatedAt.IsZero() {
		return receipt, ErrScopeDenied
	}
	raw, _ := json.Marshal(v)
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	err := t.atomic(func() error {
		var oldHash, rawOld string
		err := t.tx.QueryRowContext(t.ctx, `SELECT payload_hash,payload FROM private_project_shares WHERE id=?`, v.ID).Scan(&oldHash, &rawOld)
		if err == nil {
			if oldHash != digest || rawOld != string(raw) {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal([]byte(rawOld), &v)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		p, err := t.GetProject(v.ProjectID)
		if err != nil {
			return err
		}
		if (v.ResourceID == "") != (v.VersionID == "") {
			return ErrScopeDenied
		}
		if v.ResourceID != "" {
			resource, e := t.GetResource(p.ScopeID, v.ResourceID)
			if e != nil {
				return e
			}
			version, e := t.GetResourceVersion(p.ScopeID, v.VersionID)
			if e != nil {
				return e
			}
			if version.ResourceID != resource.ID || !version.Saved || version.ContentHash == "" || version.ContentHash != v.ResourceHash {
				return ErrConflict
			}
		}
		if err = t.RequireProjectPrincipalAccess(p.ScopeID, v.PrincipalID, time.Now().UTC()); err != nil {
			return err
		}
		member, err := t.GetProjectMember(p.ID, v.SourceRoleID)
		if err != nil || member.State != work.ProjectMemberReady || member.BotGeneration != v.SourceBotGeneration {
			return ErrScopeDenied
		}
		binding, err := t.GetRoleBotBinding(v.SourceRoleID)
		if err != nil || binding.IdentityGeneration != v.SourceBotGeneration {
			return ErrScopeDenied
		}
		leader, err := t.GetProjectMember(p.ID, p.LeaderRoleID)
		if err != nil || leader.State == work.ProjectMemberRemoved {
			return ErrScopeDenied
		}
		messageID := "private-share-message:" + v.ID
		inboxID := "private-share-inbox:" + v.ID
		body := work.NewContent("private-share-body:"+v.ID, p.ScopeID, "text/plain; charset=utf-8", []byte(v.Text), v.CreatedAt)
		if err = t.PutContent(body); err != nil {
			return err
		}
		source := work.SourceRef{Kind: "private_share", ID: v.ID, Version: v.DraftHash, EvidenceRef: v.ConsentEventID}
		msg := work.Message{ID: messageID, ScopeID: p.ScopeID, ConversationID: "private-share", Source: work.Actor{Kind: work.ActorPrincipal, ID: v.PrincipalID}, DerivedFrom: &source, BodyRef: body.ID, Revision: 1, CreatedAt: v.CreatedAt}
		input := work.Inbox{ID: inboxID, ScopeID: p.ScopeID, RecipientRoleID: p.LeaderRoleID, EventID: "message:" + messageID, MessageID: messageID, State: work.InboxPending, Revision: 1, AvailableAt: v.CreatedAt}
		if _, _, err = t.AcceptMessage(msg, []work.Inbox{input}); err != nil {
			return err
		}
		_, err = t.tx.ExecContext(t.ctx, `INSERT INTO private_project_shares(id,project_id,scope_id,principal_id,source_role_id,message_id,inbox_id,source_hash,payload,payload_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, v.ID, p.ID, p.ScopeID, v.PrincipalID, v.SourceRoleID, messageID, inboxID, body.SHA256, raw, digest, v.CreatedAt.UTC().Format(time.RFC3339Nano))
		return err
	})
	if err != nil {
		return receipt, err
	}
	var scope, contentHash string
	err = t.tx.QueryRowContext(t.ctx, `SELECT project_id,scope_id,message_id,inbox_id,source_hash FROM private_project_shares WHERE id=?`, v.ID).Scan(&receipt.ProjectID, &scope, &receipt.MessageID, &receipt.InboxID, &contentHash)
	if err != nil {
		return receipt, err
	}
	receipt.ID = v.ID
	receipt.ContentHash = contentHash
	return receipt, nil
}
func (t *Tx) authorizePrivateProjectShare(scope work.WorkScope, input work.Inbox, msg work.Message, now time.Time) error {
	var projectID, principal, role, sourceHash, payloadHash string
	var raw []byte
	err := t.tx.QueryRowContext(t.ctx, `SELECT project_id,principal_id,source_role_id,source_hash,payload,payload_hash FROM private_project_shares WHERE scope_id=? AND message_id=? AND inbox_id=?`, scope.ID, msg.ID, input.ID).Scan(&projectID, &principal, &role, &sourceHash, &raw, &payloadHash)
	if err != nil {
		return err
	}
	var v PrivateProjectShare
	payloadSum := sha256.Sum256(raw)
	if hex.EncodeToString(payloadSum[:]) != payloadHash || json.Unmarshal(raw, &v) != nil || v.ID == "" || v.PrivateOwnerID == "" || v.PrivateEpoch < 1 || v.SourceBotGeneration < 1 || v.DraftHash == "" || v.ConsentEventID == "" || v.ConsentFactHash == "" || v.ProjectID != projectID || v.PrincipalID != principal || v.SourceRoleID != role || v.Text == "" {
		return ErrConflict
	}
	p, err := t.GetProject(projectID)
	if err != nil {
		return err
	}
	if v.ResourceID != "" {
		resource, e := t.GetResource(scope.ID, v.ResourceID)
		if e != nil {
			return e
		}
		version, e := t.GetResourceVersion(scope.ID, v.VersionID)
		if e != nil {
			return e
		}
		if version.ResourceID != resource.ID || !version.Saved || version.ContentHash != v.ResourceHash {
			return ErrConflict
		}
	}
	if p.ScopeID != scope.ID || input.RecipientRoleID != p.LeaderRoleID || msg.Source != (work.Actor{Kind: work.ActorPrincipal, ID: principal}) || msg.DerivedFrom == nil || msg.DerivedFrom.Kind != "private_share" || msg.DerivedFrom.ID != v.ID || msg.DerivedFrom.Version != v.DraftHash || msg.DerivedFrom.EvidenceRef != v.ConsentEventID {
		return ErrScopeDenied
	}
	body, err := t.GetContent(scope.ID, msg.BodyRef)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(v.Text))
	if body.SHA256 != sourceHash || sourceHash != hex.EncodeToString(sum[:]) {
		return ErrConflict
	}
	if err = t.RequireProjectPrincipalAccess(scope.ID, principal, now); err != nil {
		return err
	}
	member, err := t.GetProjectMember(projectID, role)
	if err != nil || member.State != work.ProjectMemberReady || member.BotGeneration != v.SourceBotGeneration {
		return ErrScopeDenied
	}
	binding, err := t.GetRoleBotBinding(role)
	if err != nil || binding.IdentityGeneration != v.SourceBotGeneration {
		return ErrScopeDenied
	}
	return nil
}
