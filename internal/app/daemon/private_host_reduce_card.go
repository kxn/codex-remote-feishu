package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (r privateHostReducer) planLocalCard(action control.Action, contract control.FeishuFrontstageActionContract, events []eventcontract.Event, deferReplacement bool) (*privatechat.CardResponse, []eventcontract.Event, error) {
	for i, event := range events {
		if contract.CurrentCardMode == control.FeishuFrontstageCurrentCardInlineView && i == 0 && !event.InlineReplaceCurrentCard && inlineFallbackReplacementBlockedByActivePicker(events) {
			return nil, events, nil
		}
		// Snapshot projection may inspect a git worktree. Only purely local card
		// views are eligible inside the transaction; other effects remain queued.
		if event.Command != nil || event.DaemonCommand != nil || !(event.Snapshot != nil || event.PageView != nil || event.SelectionView != nil || event.Notice != nil || event.PathPickerView != nil || event.TargetPickerView != nil || event.ThreadHistoryView != nil || event.RequestView != nil) {
			continue
		}
		if strings.TrimSpace(event.GatewayID) == "" {
			event.GatewayID = action.GatewayID
		}
		if strings.TrimSpace(event.SurfaceSessionID) == "" {
			event.SurfaceSessionID = action.SurfaceSessionID
		}
		if event.SurfaceSessionID != action.SurfaceSessionID || event.GatewayID != action.GatewayID {
			continue
		}
		event.DaemonLifecycleID = r.host.daemonLifecycleID
		var ops []feishu.Operation
		if event.Snapshot != nil {
			ops = r.host.projector.ProjectStatusSnapshotWithoutWorktree(action.ChatID, event)
		} else {
			ops = r.host.projector.ProjectEvent(action.ChatID, event.Normalized())
		}
		if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard {
			continue
		}
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, nil, err
		}
		token := hex.EncodeToString(secret)
		frozen, err := feishu.FreezePrivateOperation(ops[0], r.host.daemonLifecycleID)
		if err != nil {
			return nil, nil, err
		}
		stamped, _, err := feishu.StampPrivateView(frozen, token)
		if err != nil {
			return nil, nil, err
		}
		operation, err := feishu.ThawPrivateOperation(stamped)
		if err != nil {
			return nil, nil, err
		}
		raw, err := feishu.FreezePrivateCardResponse(operation)
		if err != nil {
			return nil, nil, err
		}
		// The callback itself authenticates the current card target. Record its
		// local ownership before the same transaction checkpoints this plan.
		messageID := action.Inbound.OpenMessageID
		if event.PathPickerView != nil {
			r.host.service.RecordPathPickerMessage(action.SurfaceSessionID, event.PathPickerView.PickerID, messageID)
		}
		if event.TargetPickerView != nil {
			r.host.service.RecordTargetPickerMessage(action.SurfaceSessionID, event.TargetPickerView.PickerID, messageID)
		}
		if event.ThreadHistoryView != nil {
			r.host.service.RecordThreadHistoryMessage(action.SurfaceSessionID, event.ThreadHistoryView.PickerID, messageID)
		}
		if event.PageView != nil && event.PageView.TrackingKey != "" {
			r.host.service.RecordPageTrackingMessage(action.SurfaceSessionID, event.PageView.TrackingKey, messageID)
		}
		remaining := removeUIEventAt(events, i)
		if !contract.FollowupPolicy.Empty() {
			remaining = filterUIEventsByFollowupPolicy(remaining, contract.FollowupPolicy)
		}
		if deferReplacement {
			// The action is committed now, but its card has a previous external
			// writer. Publish this replacement after that writer's receipt instead
			// of racing the HTTP callback response against the pending PATCH.
			event.InlineReplaceCurrentCard = true
			event.SourceMessageID = messageID
			event.Meta.MessageDelivery.TargetMessageID = messageID
			event.Payload = nil
			remaining = append([]eventcontract.Event{event.Normalized()}, remaining...)
			return &privatechat.CardResponse{Version: 1, TargetMessageID: messageID, Response: json.RawMessage(`{"toast":{"type":"info","content":"已收到，正在更新这张卡片。"}}`)}, remaining, nil
		}
		return &privatechat.CardResponse{Version: 1, TargetMessageID: action.Inbound.OpenMessageID, ViewToken: token, Response: raw}, remaining, nil
	}
	return nil, events, nil
}
