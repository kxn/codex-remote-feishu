package workservice

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A real SDK quoted command keeps its receive-time context independently of
// the currently selected navigation. This does not grant another scope.
func quotedPrivateCommandContext(tx *workstore.Tx, source workstore.PrivateActionSource) (work.WorkContext, bool, error) {
	if source.CommandID == "" {
		return work.WorkContext{}, false, nil
	}
	command, err := tx.GetPrivateCommand(source.CommandID)
	if err != nil {
		return work.WorkContext{}, false, err
	}
	raw, err := tx.GetIngress(command.IngressID)
	if err != nil {
		return work.WorkContext{}, false, err
	}
	if raw.Envelope.ParentID == "" && raw.Envelope.RootID == "" && raw.Envelope.ThreadID == "" {
		return work.WorkContext{}, false, nil
	}
	if raw.Route.ReplyRoutingError != "" || raw.ReplyDeliveryError != "" || command.ContextID != raw.Route.ContextID || command.ScopeID != source.PersonalScopeID {
		return work.WorkContext{}, false, workstore.ErrScopeDenied
	}
	front, err := tx.GetContext(command.ContextID)
	if err != nil {
		return front, false, err
	}
	if front.ScopeID != command.ScopeID || front.RoleID != command.RoleID || front.Purpose == work.PurposeControl || front.Purpose == work.PurposeReview {
		return front, false, workstore.ErrScopeDenied
	}
	return front, true, nil
}
