package workstore

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// An inline card keeps one platform message ID across updates. Its quoted
// content has no immutable platform revision, so cross-context or uncertain
// replacement cannot acquire a context merely from the original mapping.
func (t *Tx) privateReplyPublicationContext(op work.Operation, record work.IngressRecord) (string, bool, error) {
	body, err := t.GetContent(op.ScopeID, op.RequestRef)
	if err != nil {
		return "", false, err
	}
	var projection struct {
		UIViewID         string `json:"ui_view_id"`
		ReplaceMessageID string `json:"replace_message_id"`
	}
	if err = json.Unmarshal(body.Data, &projection); err != nil {
		return "", false, err
	}
	if projection.UIViewID == "" {
		return op.ContextID, true, nil
	}
	original, err := t.GetUIView(projection.UIViewID)
	if err != nil {
		return "", false, err
	}
	if original.ScopeID != op.ScopeID || original.OperationID != op.ID || original.ContextID != op.ContextID {
		return "", false, ErrScopeDenied
	}
	views, err := t.ListUIViews(op.ScopeID)
	if err != nil {
		return "", false, err
	}
	for _, candidate := range views {
		if candidate.ID == original.ID || candidate.Tenant != original.Tenant || candidate.ChatID != original.ChatID || candidate.Binding != original.Binding || candidate.ReplacesViewID == "" {
			continue
		}
		candidateOp, e := t.GetOperation(candidate.OperationID)
		if e != nil {
			return "", false, e
		}
		candidateBody, e := t.GetContent(op.ScopeID, candidateOp.RequestRef)
		if e != nil {
			return "", false, e
		}
		var update struct {
			ReplaceMessageID string `json:"replace_message_id"`
		}
		if e = json.Unmarshal(candidateBody.Data, &update); e != nil {
			return "", false, e
		}
		if update.ReplaceMessageID != original.ExternalMessageID || update.ReplaceMessageID == "" || candidateOp.State == work.OperationTerminal {
			continue
		}
		if candidate.ContextID != original.ContextID || candidateOp.State != work.OperationConfirmed || candidate.ExternalMessageID != original.ExternalMessageID {
			return "", false, nil
		}
	}
	return original.ContextID, true, nil
}
