package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"sort"
)

// A recall notice, automation event or helper message must not turn staged
// human attachments into a fresh model request. Resolve actual message actors.
func (t *Tx) MediaBatchHasHumanInstruction(inputs []work.Inbox) (bool, error) {
	staged := false
	for _, input := range inputs {
		staged = staged || input.AwaitingText
	}
	if !staged {
		return true, nil
	}
	verified := append([]work.Inbox(nil), inputs...)
	for i, input := range verified {
		if input.AwaitingText || input.MessageID == "" {
			continue
		}
		message, err := t.GetMessage(input.MessageID)
		if err != nil {
			return false, err
		}
		scope, err := t.GetScope(input.ScopeID)
		if err != nil {
			return false, err
		}
		if message.ScopeID != input.ScopeID || message.Source.Kind != work.ActorPrincipal || scope.Kind != work.ScopePersonal || message.Source.ID != scope.PrincipalID || message.RecalledEvidenceRef != "" {
			verified[i].MessageID = ""
		}
	}
	return work.MediaBatchHasInstruction(verified), nil
}

// MediaBatchWithinBudget shares the native prompt's durable attachment limits.
func (t *Tx) MediaBatchWithinBudget(inputs []work.Inbox) (bool, error) {
	seen := map[string]bool{}
	count := 0
	var size int64
	for _, input := range inputs {
		if input.MessageID == "" {
			continue
		}
		message, err := t.GetMessage(input.MessageID)
		if err != nil {
			return false, err
		}
		for _, asset := range message.Attachments {
			if seen[asset.ContentRef] {
				continue
			}
			seen[asset.ContentRef] = true
			count++
			size += asset.Size
			if count > 64 || size > 64<<20 {
				return false, nil
			}
		}
	}
	return true, nil
}

// StagedMediaCluster resolves the attachment-only messages belonging to this
// next instruction, excluding batches already bound to earlier human text.
func (t *Tx) StagedMediaCluster(input work.Inbox) ([]work.Inbox, error) {
	return t.pendingInputMediaCluster(input, true)
}

// PendingInputMediaCluster freezes the exact pending media preceding one human
// request. Historical requests are boundaries even when already claimed or
// cancelled; their images must never be reassigned to a later request.
func (t *Tx) PendingInputMediaCluster(input work.Inbox) ([]work.Inbox, error) {
	return t.pendingInputMediaCluster(input, false)
}
func (t *Tx) pendingInputMediaCluster(input work.Inbox, budgetObservation bool) ([]work.Inbox, error) {
	anchor, err := t.GetInbox(input.ID)
	if err != nil {
		return nil, err
	}
	if anchor.State != work.InboxPending {
		return nil, ErrConflict
	}
	scope, err := t.GetScope(anchor.ScopeID)
	if err != nil {
		return nil, err
	}
	inputs, err := listRecords[work.Inbox](t, "inbox", " WHERE scope_id=? AND context_id=? AND role_id=?", anchor.ScopeID, anchor.RecipientContextID, anchor.RecipientRoleID)
	if err != nil {
		return nil, err
	}
	before := func(a, b work.Inbox) bool {
		if a.AvailableAt.Equal(b.AvailableAt) {
			return a.ID < b.ID
		}
		return a.AvailableAt.Before(b.AvailableAt)
	}
	beforeAnchor := func(other work.Inbox) bool {
		return before(other, anchor) || (budgetObservation && other.AvailableAt.Equal(anchor.AvailableAt))
	}
	var boundary work.Inbox
	for _, other := range inputs {
		if other.ID == anchor.ID || other.AwaitingText || !beforeAnchor(other) || other.MessageID == "" || other.ControlOfInputID != "" || other.AutomationEpisodeID != "" || other.SessionGoalIntentID != "" {
			continue
		}
		message, e := t.GetMessage(other.MessageID)
		if e != nil {
			return nil, e
		}
		if message.Source.Kind == work.ActorPrincipal && message.Source.ID == scope.PrincipalID && (boundary.ID == "" || before(boundary, other)) {
			boundary = other
		}
	}
	var cluster []work.Inbox
	for _, other := range inputs {
		if other.State != work.InboxPending {
			continue
		}
		if other.ID == anchor.ID || (other.AwaitingText && beforeAnchor(other) && (boundary.ID == "" || before(boundary, other))) {
			cluster = append(cluster, other)
		}
	}
	sort.SliceStable(cluster, func(i, j int) bool { return before(cluster[i], cluster[j]) })
	return cluster, nil
}
