package workpublish

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func publicationRepliesInThread(tx *workstore.Tx, scope work.WorkScope, projection workservice.MessageProjection) (bool, error) {
	if scope.Kind != work.ScopeProject && scope.Kind != work.ScopeBootstrap {
		return false, nil
	}
	if projection.TaskID != "" {
		return true, nil
	}
	if projection.ReplyToMessageID == "" {
		return false, nil
	}
	// A quoted main-chat message also has a parent/root association. Only the
	// SDK's actual thread evidence or an owned task topic selects thread mode.
	raw, err := tx.GetIngressByPlatformMessage(projection.Tenant, projection.ChatID, projection.ReplyToMessageID)
	if err == nil {
		return raw.Envelope.ThreadID != "", nil
	}
	if !errors.Is(err, workstore.ErrNotFound) {
		return false, err
	}
	mapping, err := tx.GetPlatformMessageMapping(projection.Tenant, projection.ChatID, projection.ReplyToMessageID)
	if errors.Is(err, workstore.ErrNotFound) {
		return false, nil
	}
	return mapping.TaskID != "", err
}
