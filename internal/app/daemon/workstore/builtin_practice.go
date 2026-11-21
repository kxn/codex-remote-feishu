package workstore

import (
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"time"
)

// InstallBuiltinPractice is a trusted composition-time package installer, never
// a model-facing publication API. It creates its own body from shipped bytes;
// existing private/project content references are deliberately ignored.
func (t *Tx) InstallBuiltinPractice(v work.PracticeDefinition, shippedBody []byte, at time.Time) error {
	return t.atomic(func() error {
		key := practiceKey(v.Owner, v.PracticeRef)
		content := work.NewContent("builtin:"+key, v.StorageScopeID, "text/markdown", shippedBody, at)
		v.ContentRef = content.ID
		v.SharingGrantID = ""
		v.Source = work.SourceRef{Kind: "builtin_package", ID: v.ID, Version: v.Version, EvidenceRef: content.ID}
		if err := t.PutContent(content); err != nil {
			return err
		}
		var existing work.PracticeDefinition
		if err := t.read("practices", key, &existing); err == nil {
			if !reflect.DeepEqual(existing, v) {
				return ErrIdempotencyConflict
			}
			return t.practiceReadable(existing)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := t.putPractice(v, 0, true); err != nil {
			return err
		}
		_, err := t.tx.ExecContext(t.ctx, "INSERT INTO trusted_practices(practice_key,content_hash) VALUES(?,?)", key, content.SHA256)
		return err
	})
}
func (t *Tx) practiceReadable(v work.PracticeDefinition) error {
	if v.SharingGrantID != "" {
		return t.validateSharing(v.StorageScopeID, v.ContentRef, v.SharingGrantID, practiceSharingTarget(v))
	}
	var hash string
	err := t.tx.QueryRowContext(t.ctx, "SELECT content_hash FROM trusted_practices WHERE practice_key=?", practiceKey(v.Owner, v.PracticeRef)).Scan(&hash)
	if err != nil {
		return fmt.Errorf("practice has no publication grant or trusted package receipt")
	}
	content, err := t.GetContent(v.StorageScopeID, v.ContentRef)
	if err != nil {
		return err
	}
	if content.SHA256 != hash {
		return fmt.Errorf("builtin package digest mismatch")
	}
	return nil
}
