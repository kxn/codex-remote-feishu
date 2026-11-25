package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const queuedMessageStartedText = "开始执行这条排队消息。"

const privateCarrierRecoveryWait = "private_carrier_recovery"

type dispatchNextOptions struct {
	suppressQueuedStartNoticeQueueItemID string
}

func (opts dispatchNextOptions) suppressesQueuedStartNotice(item *state.QueueItemRecord) bool {
	return item != nil && strings.TrimSpace(opts.suppressQueuedStartNoticeQueueItemID) == strings.TrimSpace(item.ID)
}

func (s *Service) queuedMessageStartedEvent(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord) *eventcontract.Event {
	if surface == nil || item == nil || item.SourceKind != state.QueueItemSourceUser || item.WaitReason == privateCarrierRecoveryWait {
		return nil
	}
	replyToMessageID := strings.TrimSpace(xutil.FirstNonEmpty(item.ReplyToMessageID, item.SourceMessageID))
	if replyToMessageID == "" {
		return nil
	}
	replyToMessagePreview := strings.TrimSpace(xutil.FirstNonEmpty(item.ReplyToMessagePreview, item.SourceMessagePreview))
	return &eventcontract.Event{
		Kind:                 eventcontract.KindTimelineText,
		GatewayID:            surface.GatewayID,
		SurfaceSessionID:     surface.SurfaceSessionID,
		SourceMessageID:      replyToMessageID,
		SourceMessagePreview: replyToMessagePreview,
		TimelineText: &control.TimelineText{
			ThreadID:              queuedItemExecutionThreadID(item),
			Type:                  control.TimelineTextQueuedMessageStarted,
			Text:                  queuedMessageStartedText,
			ReplyToMessageID:      replyToMessageID,
			ReplyToMessagePreview: replyToMessagePreview,
		},
	}
}

func (s *Service) privateRecoveryInputNotice(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord) *eventcontract.Event {
	if surface == nil || item == nil || item.SourceKind != state.QueueItemSourceUser || strings.TrimSpace(item.SourceMessageID) == "" {
		return nil
	}
	if _, ok := s.PrivateReplacementInputTarget(surface.SurfaceSessionID); !ok {
		return nil
	}
	if surface.ActiveQueueItemID == "" && len(surface.QueuedQueueItemIDs) == 0 {
		item.WaitReason = privateCarrierRecoveryWait
	}
	if surface.PendingHeadless.RecoveryInputNoticeSent {
		return nil
	}
	surface.PendingHeadless.RecoveryInputNoticeSent = true
	return &eventcontract.Event{
		Kind: eventcontract.KindTimelineText, GatewayID: surface.GatewayID, SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID: item.SourceMessageID, SourceMessagePreview: item.SourceMessagePreview,
		TimelineText: &control.TimelineText{Type: control.TimelineTextRecoveryInputWaiting, Text: "后台正在恢复，已收到这条消息，恢复完成后会自动执行。", ReplyToMessageID: item.SourceMessageID, ReplyToMessagePreview: item.SourceMessagePreview},
	}
}

// PrivateRecoveryNoticeObsolete observes the current durable input projection.
// A missing input was removed by a successful route discard; it must not
// produce a late acknowledgement in a different conversation.
func (s *Service) PrivateRecoveryNoticeObsolete(surfaceID, source string) bool {
	if !s.config.PrivateApplication || strings.TrimSpace(source) == "" {
		return false
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return false
	}
	for _, item := range surface.QueueItems {
		if item != nil && item.SourceMessageID == source {
			return item.Status == state.QueueItemCompleted || item.Status == state.QueueItemFailed || item.Status == state.QueueItemDiscarded
		}
	}
	return true
}
