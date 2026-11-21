package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

// Memory corrections carry only an opaque entry/version pointer into each
// authorized context. Private source text never crosses into another scope.
func (t *Tx) notifyMemoryCorrection(old, current work.MemoryEntry) error {
	workers, err := listRecords[work.WorkerSession](t, "workers", " WHERE json_extract(payload,'$.state')<>'retired'")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, worker := range workers {
		if worker.Controller != work.ControllerAutomatic || seen[worker.ContextID] {
			continue
		}
		seen[worker.ContextID] = true
		wc, err := t.GetContext(worker.ContextID)
		if err != nil {
			return err
		}
		if wc.Purpose == work.PurposeControl || wc.Purpose == work.PurposeReview {
			continue
		}
		read := work.ReadScope{ScopeID: wc.ScopeID, ContextID: wc.ID, TaskID: wc.TaskID, RoleID: wc.RoleID}
		scope, err := t.readScope(read)
		if err == ErrScopeDenied {
			continue
		}
		if err != nil {
			return err
		}
		if !memoryAllowed(old.Scope, read, scope) {
			continue
		}
		if (old.Scope.Kind == work.MemoryRole || old.Scope.Kind == work.MemoryTeam) && t.validateSharing(old.StorageScopeID, old.ContentRef, old.SharingGrantID, memorySharingTarget(old)) != nil {
			continue
		}
		id := fmt.Sprintf("memory-correction:%s:%d:%s", old.ID, current.Revision, wc.ID)
		if _, err = t.GetMessage(id); err == nil {
			continue
		} else if err != ErrNotFound {
			return err
		}
		now := current.UpdatedAt
		if now.IsZero() {
			now = time.Now().UTC()
		}
		status := "已修订"
		if current.State != work.MemoryValid {
			status = "已失效"
		}
		text := fmt.Sprintf("记忆 %s %s，当前版本 %d。旧版本 %d 不应继续作为依据。请通过 memory.read 核对当前资料；失效或不可读的记忆不得沿用。这是内部修订通知，无需单独打扰用户；若影响已给出的结果，请说明需要更正的部分。此次更新不代表原生执行已停止，也不会撤销已发生的工作。", old.ID, status, current.Revision, old.Revision)
		body := work.NewContent(id+":body", wc.ScopeID, "text/plain", []byte(text), now)
		if err = t.PutContent(body); err != nil {
			return err
		}
		// The source identifies this local notice, not the foreign private body.
		source := work.SourceRef{Kind: "memory_revision_notice", ID: old.ID, Version: fmt.Sprint(current.Revision), EvidenceRef: body.ID}
		message := work.Message{ID: id, ScopeID: wc.ScopeID, ConversationID: wc.ID, Source: work.Actor{Kind: work.ActorSystem, ID: "memory-correction"}, DerivedFrom: &source, BodyRef: body.ID, Revision: 1, CreatedAt: now}
		input := work.Inbox{ID: id + ":input", ScopeID: wc.ScopeID, RecipientRoleID: wc.RoleID, RecipientContextID: wc.ID, EventID: id, MessageID: id, State: work.InboxPending, Revision: 1, AvailableAt: now}
		if _, _, err = t.AcceptMessage(message, []work.Inbox{input}); err != nil {
			return err
		}
	}
	return nil
}
