package workstore

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PublicationWasReanchored recognizes a frozen historical unknown publication
// whose future dispatch was explicitly sealed by an authenticated human action.
// It never changes that unknown receipt into sent or clears its reservation.
func (t *Tx) PublicationWasReanchored(op work.Operation) (bool, error) {
	if op.Kind != "feishu.publish" || (op.State != work.OperationUnknown && op.State != work.OperationTerminal && op.State != work.OperationRetryable) {
		return false, nil
	}
	for _, step := range op.Steps {
		if step.ID != "topic_reanchored" || step.State != work.OperationConfirmed || step.Receipt == "" {
			continue
		}
		audit, err := t.GetOperation(step.Receipt)
		if err != nil {
			return false, err
		}
		if audit.Kind != "task.topic.reanchor" || audit.ScopeID != op.ScopeID || audit.State != work.OperationConfirmed || audit.Actor.Kind != work.ActorPrincipal || audit.ExternalReceipt == "" {
			return false, ErrScopeDenied
		}
		body, err := t.GetContent(op.ScopeID, audit.RequestRef)
		if err != nil {
			return false, err
		}
		if body.SHA256 != audit.RequestHash {
			return false, ErrScopeDenied
		}
		var req struct {
			InteractionID, ViewID, OutboxID, OperationID, TaskID string
			OperationRevision, OutboxRevision, TopicRevision     int64
		}
		if err = json.Unmarshal(body.Data, &req); err != nil {
			return false, err
		}
		if req.OperationID != op.ID || req.TaskID != audit.TargetID || req.InteractionID != audit.IdempotencyKey || req.OperationRevision < 1 {
			return false, ErrScopeDenied
		}
		item, err := t.GetOutbox(req.OutboxID)
		if err != nil {
			return false, err
		}
		if item.OperationID != op.ID || item.ScopeID != op.ScopeID {
			return false, ErrScopeDenied
		}
		interaction, err := t.GetHumanInteraction(req.InteractionID)
		if err != nil {
			return false, err
		}
		view, err := t.GetUIView(req.ViewID)
		if err != nil {
			return false, err
		}
		if interaction.PrincipalID != audit.Actor.ID || interaction.Envelope.Values["team_view"] != view.ID || interaction.Envelope.Values["action"] != "publication.reanchor" || view.ScopeID != op.ScopeID || view.Kind != "publication.recovery" || view.TargetID != item.ID || view.TargetRevision != req.OperationRevision || view.ChatID != interaction.Envelope.ChatID || view.Tenant != interaction.Envelope.Tenant || interaction.Envelope.MessageID != audit.ExternalReceipt {
			return false, ErrScopeDenied
		}
		mapping, err := t.GetPlatformMessageMapping(view.Tenant, view.ChatID, audit.ExternalReceipt)
		if err != nil {
			return false, err
		}
		if mapping.OperationID != view.OperationID || mapping.ScopeID != op.ScopeID || mapping.RoleID != view.Binding.RoleID || mapping.BotGeneration != view.Binding.IdentityGeneration {
			return false, ErrScopeDenied
		}
		return true, nil
	}
	return false, nil
}
