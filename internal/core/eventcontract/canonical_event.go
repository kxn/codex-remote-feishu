package eventcontract

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (event Event) CanonicalKind() Kind {
	if kind := PayloadKind(event.Payload); kind != KindUnknown {
		return kind
	}
	if kind := canonicalRootKind(event); kind != KindUnknown {
		return kind
	}
	if IsKnownKind(event.Kind) {
		return event.Kind
	}
	return KindUnknown
}

func canonicalRootKind(event Event) Kind {
	switch {
	case event.Command != nil:
		return KindAgentCommand
	case event.DaemonCommand != nil:
		return KindDaemonCommand
	case event.Block != nil:
		return KindBlockCommitted
	case event.ExecCommandProgress != nil:
		return KindExecCommandProgress
	case event.ImageOutput != nil:
		return KindImageOutput
	case event.TimelineText != nil:
		return KindTimelineText
	case event.PlanUpdate != nil:
		return KindPlanUpdate
	case event.Notice != nil:
		return KindNotice
	case event.PendingInput != nil:
		return KindPendingInput
	case event.ThreadHistoryView != nil:
		return KindThreadHistory
	case event.TargetPickerView != nil:
		return KindTargetPicker
	case event.PathPickerView != nil:
		return KindPathPicker
	case event.RequestView != nil:
		return KindRequest
	case event.PageView != nil:
		return KindPage
	case event.SelectionView != nil:
		return KindSelection
	case event.Snapshot != nil:
		return KindSnapshot
	default:
		return KindUnknown
	}
}

func (event Event) CanonicalPayload() Payload {
	if event.Payload != nil {
		return event.Payload
	}
	switch event.CanonicalKind() {
	case KindSnapshot:
		if event.Snapshot != nil {
			return SnapshotPayload{Snapshot: *event.Snapshot}
		}
		return SnapshotPayload{}
	case KindSelection:
		if event.SelectionView != nil {
			return SelectionPayload{
				View:    *event.SelectionView,
				Context: cloneSelectionContext(event.SelectionContext),
			}
		}
		return SelectionPayload{Context: cloneSelectionContext(event.SelectionContext)}
	case KindPage:
		if event.PageView != nil {
			return PagePayload{
				View:    *event.PageView,
				Context: clonePageContext(event.PageContext),
			}
		}
		return PagePayload{Context: clonePageContext(event.PageContext)}
	case KindRequest:
		if event.RequestView != nil {
			return RequestPayload{
				View:    *event.RequestView,
				Context: cloneRequestContext(event.RequestContext),
			}
		}
		return RequestPayload{Context: cloneRequestContext(event.RequestContext)}
	case KindPathPicker:
		if event.PathPickerView != nil {
			return PathPickerPayload{
				View:    *event.PathPickerView,
				Context: clonePathPickerContext(event.PathPickerContext),
			}
		}
		return PathPickerPayload{Context: clonePathPickerContext(event.PathPickerContext)}
	case KindTargetPicker:
		if event.TargetPickerView != nil {
			return TargetPickerPayload{
				View:    *event.TargetPickerView,
				Context: cloneTargetPickerContext(event.TargetPickerContext),
			}
		}
		return TargetPickerPayload{Context: cloneTargetPickerContext(event.TargetPickerContext)}
	case KindThreadHistory:
		if event.ThreadHistoryView != nil {
			return ThreadHistoryPayload{
				View:    *event.ThreadHistoryView,
				Context: cloneThreadHistoryContext(event.ThreadHistoryContext),
			}
		}
		return ThreadHistoryPayload{Context: cloneThreadHistoryContext(event.ThreadHistoryContext)}
	case KindPendingInput:
		if event.PendingInput != nil {
			return PendingInputPayload{State: *event.PendingInput}
		}
		return PendingInputPayload{}
	case KindNotice:
		payload := NoticePayload{
			ThreadSelection: cloneThreadSelection(event.ThreadSelection),
		}
		if event.Notice != nil {
			payload.Notice = *event.Notice
		}
		return payload
	case KindPlanUpdate:
		if event.PlanUpdate != nil {
			return PlanUpdatePayload{PlanUpdate: *event.PlanUpdate}
		}
		return PlanUpdatePayload{}
	case KindBlockCommitted:
		payload := BlockCommittedPayload{
			FileChangeSummary: cloneFileChangeSummary(event.FileChangeSummary),
			TurnDiffSnapshot:  cloneTurnDiffSnapshot(event.TurnDiffSnapshot),
			FinalTurnSummary:  cloneFinalTurnSummary(event.FinalTurnSummary),
		}
		if event.Block != nil {
			payload.Block = *event.Block
		}
		return payload
	case KindTimelineText:
		if event.TimelineText != nil {
			return TimelineTextPayload{TimelineText: *event.TimelineText}
		}
		return TimelineTextPayload{}
	case KindImageOutput:
		if event.ImageOutput != nil {
			return ImageOutputPayload{ImageOutput: *event.ImageOutput}
		}
		return ImageOutputPayload{}
	case KindExecCommandProgress:
		if event.ExecCommandProgress != nil {
			return ExecCommandProgressPayload{Progress: *event.ExecCommandProgress}
		}
		return ExecCommandProgressPayload{}
	case KindAgentCommand:
		if event.Command != nil {
			return AgentCommandPayload{Command: *event.Command}
		}
		return AgentCommandPayload{}
	case KindDaemonCommand:
		if event.DaemonCommand != nil {
			return DaemonCommandPayload{Command: *event.DaemonCommand}
		}
		return DaemonCommandPayload{}
	default:
		return nil
	}
}

func (event Event) CanonicalSemantics() DeliverySemantics {
	semantics := event.Meta.Semantics.Normalized()
	if semantics != (DeliverySemantics{}) {
		return semantics
	}
	kind := event.CanonicalKind()
	payload := event.CanonicalPayload()
	semantics = DeliverySemantics{
		VisibilityClass:        canonicalVisibilityClass(kind, payload),
		HandoffClass:           canonicalHandoffClass(kind, payload),
		FirstResultDisposition: FirstResultDispositionKeep,
		OwnerCardDisposition:   OwnerCardDispositionKeep,
	}
	switch semantics.HandoffClass {
	case HandoffClassNotice, HandoffClassThreadSelection:
		semantics.FirstResultDisposition = FirstResultDispositionDrop
		semantics.OwnerCardDisposition = OwnerCardDispositionDrop
	}
	return semantics.Normalized()
}

func (event Event) CanonicalMessageDelivery() MessageDelivery {
	delivery := event.Meta.MessageDelivery.Normalized()
	if delivery != (MessageDelivery{}) {
		return delivery
	}
	switch event.CanonicalKind() {
	case KindBlockCommitted, KindTimelineText:
		return MessageDelivery{
			FirstSendLane: MessageLaneReplyThread,
			Mutation:      MessageMutationAppendOnly,
		}
	case KindPage, KindPathPicker, KindTargetPicker, KindThreadHistory, KindExecCommandProgress:
		return MessageDelivery{
			FirstSendLane: MessageLaneTopLevel,
			Mutation:      MessageMutationPatchSameMessage,
		}
	case KindSnapshot,
		KindNotice,
		KindPlanUpdate,
		KindSelection,
		KindRequest,
		KindPendingInput,
		KindImageOutput:
		return MessageDelivery{
			FirstSendLane: MessageLaneTopLevel,
			Mutation:      MessageMutationAppendOnly,
		}
	default:
		return MessageDelivery{}
	}
}

func FilterEventsByFollowupPolicy(events []Event, policy FollowupPolicy) []Event {
	if len(events) == 0 {
		return nil
	}
	policy = policy.Normalized()
	if policy.Empty() {
		return append([]Event(nil), events...)
	}
	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if policy.ShouldDropHandoffClass(string(event.CanonicalSemantics().HandoffClass)) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func canonicalVisibilityClass(kind Kind, payload Payload) VisibilityClass {
	switch kind {
	case KindPlanUpdate:
		return VisibilityClassPlan
	case KindExecCommandProgress:
		return VisibilityClassProgressText
	case KindBlockCommitted:
		if committed, ok := payload.(BlockCommittedPayload); ok && committed.Block.Final {
			return VisibilityClassAlwaysVisible
		}
		return VisibilityClassProgressText
	case KindTimelineText, KindRequest, KindImageOutput:
		return VisibilityClassAlwaysVisible
	case KindNotice:
		if noticePayload, ok := payload.(NoticePayload); ok && noticeIsAlwaysVisible(noticePayload.Notice) {
			return VisibilityClassAlwaysVisible
		}
		return VisibilityClassUINavigation
	case KindSnapshot,
		KindSelection,
		KindPage,
		KindPathPicker,
		KindTargetPicker,
		KindThreadHistory,
		KindPendingInput:
		return VisibilityClassUINavigation
	default:
		return VisibilityClassDefault
	}
}

func canonicalHandoffClass(kind Kind, payload Payload) HandoffClass {
	switch kind {
	case KindNotice:
		if noticePayload, ok := payload.(NoticePayload); ok && noticePayload.ThreadSelection != nil {
			return HandoffClassThreadSelection
		}
		return HandoffClassNotice
	case KindSnapshot,
		KindSelection,
		KindPage,
		KindPathPicker,
		KindTargetPicker,
		KindThreadHistory,
		KindPendingInput:
		return HandoffClassNavigation
	case KindExecCommandProgress, KindPlanUpdate:
		return HandoffClassProcessDetail
	case KindBlockCommitted:
		if committed, ok := payload.(BlockCommittedPayload); ok && committed.Block.Final {
			return HandoffClassTerminalContent
		}
		return HandoffClassProcessDetail
	case KindTimelineText, KindRequest, KindImageOutput:
		return HandoffClassTerminalContent
	default:
		return HandoffClassDefault
	}
}

func noticeIsAlwaysVisible(notice control.Notice) bool {
	theme := strings.ToLower(strings.TrimSpace(notice.ThemeKey))
	code := strings.ToLower(strings.TrimSpace(notice.Code))
	title := strings.TrimSpace(notice.Title)
	text := strings.TrimSpace(notice.Text)
	switch {
	case theme == "error" || strings.Contains(theme, "error") || strings.Contains(theme, "fail"):
		return true
	case strings.Contains(code, "error"), strings.Contains(code, "failed"), strings.Contains(code, "rejected"), strings.Contains(code, "offline"), strings.Contains(code, "expired"), strings.Contains(code, "invalid"):
		return true
	case strings.Contains(title, "错误"), strings.Contains(title, "失败"), strings.Contains(title, "无法"), strings.Contains(title, "拒绝"), strings.Contains(title, "离线"), strings.Contains(title, "过期"), strings.Contains(title, "失效"):
		return true
	case strings.Contains(text, "链路错误"), strings.Contains(text, "创建失败"), strings.Contains(text, "连接失败"):
		return true
	default:
		return false
	}
}

func cloneSelectionContext(context *control.FeishuUISelectionContext) *control.FeishuUISelectionContext {
	if context == nil {
		return nil
	}
	cloned := *context
	return &cloned
}

func clonePageContext(context *control.FeishuUIPageContext) *control.FeishuUIPageContext {
	if context == nil {
		return nil
	}
	cloned := *context
	return &cloned
}

func cloneRequestContext(context *control.FeishuUIRequestContext) *control.FeishuUIRequestContext {
	if context == nil {
		return nil
	}
	cloned := *context
	return &cloned
}

func clonePathPickerContext(context *control.FeishuUIPathPickerContext) *control.FeishuUIPathPickerContext {
	if context == nil {
		return nil
	}
	cloned := *context
	return &cloned
}

func cloneTargetPickerContext(context *control.FeishuUITargetPickerContext) *control.FeishuUITargetPickerContext {
	if context == nil {
		return nil
	}
	cloned := *context
	return &cloned
}

func cloneThreadHistoryContext(context *control.FeishuUIThreadHistoryContext) *control.FeishuUIThreadHistoryContext {
	if context == nil {
		return nil
	}
	cloned := *context
	return &cloned
}

func cloneThreadSelection(selection *control.ThreadSelectionChanged) *control.ThreadSelectionChanged {
	if selection == nil {
		return nil
	}
	cloned := *selection
	return &cloned
}

func cloneFileChangeSummary(summary *control.FileChangeSummary) *control.FileChangeSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	if len(summary.Files) != 0 {
		cloned.Files = append([]control.FileChangeSummaryEntry(nil), summary.Files...)
	}
	return &cloned
}

func cloneTurnDiffSnapshot(snapshot *control.TurnDiffSnapshot) *control.TurnDiffSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	return &cloned
}

func cloneFinalTurnSummary(summary *control.FinalTurnSummary) *control.FinalTurnSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	return &cloned
}
