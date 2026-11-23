package workdisplay

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

// Ordinary queued input is acknowledged by its original message reaction, as
// in the private chat UI. Only actionable media problems need another card.
func QueuePrivateInputFeedback(tx *workstore.Tx, scope work.WorkScope, input work.Inbox, source work.IngressRecord, now time.Time) error {
	if scope.Kind != work.ScopePersonal || source.Envelope.ChatType != "p2p" || input.RecipientContextID == "" || source.Route.ScopeID != scope.ID {
		return nil
	}
	_, err := queueMediaInputFeedback(tx, scope, input, source, now)
	return err
}
