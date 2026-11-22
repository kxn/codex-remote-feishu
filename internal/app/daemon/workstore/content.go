package workstore

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

var ErrScopeDenied = errors.New("work reference is outside authorized scope")

func (t *Tx) PutContent(v work.Content) error {
	if err := v.Validate(); err != nil {
		return err
	}
	old, err := t.GetContent(v.ScopeID, v.ID)
	if err == nil {
		if old.MediaType != v.MediaType || old.SHA256 != v.SHA256 || !bytes.Equal(old.Data, v.Data) {
			return ErrIdempotencyConflict
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO content(id,scope_id,media_type,sha256,data,created_at) VALUES(?,?,?,?,?,?)", v.ID, v.ScopeID, v.MediaType, v.SHA256, v.Data, v.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}
func (t *Tx) GetContent(scopeID, id string) (work.Content, error) {
	var v work.Content
	var at string
	err := t.tx.QueryRowContext(t.ctx, "SELECT id,scope_id,media_type,sha256,data,created_at FROM content WHERE id=?", id).Scan(&v.ID, &v.ScopeID, &v.MediaType, &v.SHA256, &v.Data, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if v.ScopeID != scopeID {
		return work.Content{}, ErrScopeDenied
	}
	return validateStoredContent(v, at)
}

func validateStoredContent(content work.Content, createdAt string) (work.Content, error) {
	var err error
	content.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return content, err
	}
	if err = content.Validate(); err != nil {
		return work.Content{}, fmt.Errorf("stored content integrity: %w", err)
	}
	return content, nil
}
func (t *Tx) requireContent(scopeID, ref string) error {
	_, err := t.GetContent(scopeID, ref)
	return err
}
func (t *Tx) scopedRead(table, scopeID, id string, destination any) error {
	var actual string
	err := t.tx.QueryRowContext(t.ctx, "SELECT scope_id FROM "+table+" WHERE id=?", id).Scan(&actual)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if actual != scopeID {
		return ErrScopeDenied
	}
	return t.read(table, id, destination)
}
