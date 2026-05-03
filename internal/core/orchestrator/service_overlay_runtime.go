package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type surfaceRouteMutationBlockKind string

const (
	surfaceRouteMutationBlockNone           surfaceRouteMutationBlockKind = ""
	surfaceRouteMutationBlockTargetPicker   surfaceRouteMutationBlockKind = "target_picker"
	surfaceRouteMutationBlockPathPicker     surfaceRouteMutationBlockKind = "path_picker"
	surfaceRouteMutationBlockRequestCapture surfaceRouteMutationBlockKind = "request_capture"
	surfaceRouteMutationBlockPendingRequest surfaceRouteMutationBlockKind = "pending_request"
	surfaceRouteMutationBlockReviewRunning  surfaceRouteMutationBlockKind = "review_running"
)

type surfaceOverlayRouteCleanupOptions struct {
	PreserveTargetPicker  bool
	ForceClearReviewState bool
}

func (s *Service) surfaceRouteMutationBlock(surface *state.SurfaceConsoleRecord) surfaceRouteMutationBlockKind {
	if surface == nil {
		return surfaceRouteMutationBlockNone
	}
	review := s.activeReviewSession(surface)
	switch {
	case s.targetPickerHasBlockingProcessing(surface):
		return surfaceRouteMutationBlockTargetPicker
	case s.activePathPicker(surface) != nil:
		return surfaceRouteMutationBlockPathPicker
	case surface.ActiveRequestCapture != nil:
		return surfaceRouteMutationBlockRequestCapture
	case activePendingRequest(surface) != nil:
		return surfaceRouteMutationBlockPendingRequest
	case review != nil && strings.TrimSpace(review.ActiveTurnID) != "":
		return surfaceRouteMutationBlockReviewRunning
	default:
		return surfaceRouteMutationBlockNone
	}
}

func (s *Service) surfaceHasRouteMutationBlocker(surface *state.SurfaceConsoleRecord) bool {
	return s.surfaceRouteMutationBlock(surface) != surfaceRouteMutationBlockNone
}

func (s *Service) blockRouteMutation(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	switch s.surfaceRouteMutationBlock(surface) {
	case surfaceRouteMutationBlockTargetPicker:
		return notice(surface, "target_picker_processing", s.targetPickerProcessingBlockedText(surface))
	case surfaceRouteMutationBlockPathPicker:
		return notice(surface, "path_picker_active", "当前正在进行路径选择，请先在卡片里确认或取消；如需查看状态，可继续使用 /status。")
	case surfaceRouteMutationBlockRequestCapture:
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	case surfaceRouteMutationBlockPendingRequest:
		return notice(surface, "request_pending", pendingRequestNoticeText(activePendingRequest(surface)))
	case surfaceRouteMutationBlockReviewRunning:
		return notice(surface, "review_route_mutation_running", "当前审阅请求正在执行，暂时不能切换工作目标。请等待完成，或先 /stop 结束当前审阅。")
	default:
		return nil
	}
}

func (s *Service) cleanupContextBoundSurfaceOverlays(surface *state.SurfaceConsoleRecord, cause string, options surfaceOverlayRouteCleanupOptions) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	var events []eventcontract.Event
	pathPickerOwnedByTargetPicker := s.pathPickerOwnedByActiveTargetPicker(surface)
	events = append(events, s.sealPathPickerForContextChange(surface, cause)...)
	if !options.PreserveTargetPicker {
		if pathPickerOwnedByTargetPicker {
			s.clearTargetPickerRuntime(surface)
		} else {
			events = append(events, s.sealTargetPickerForContextChange(surface, cause)...)
		}
	}
	events = append(events, s.sealThreadHistoryForContextChange(surface, cause)...)
	events = append(events, s.sealReviewCommitPickerForContextChange(surface, cause)...)
	events = append(events, s.sealWorkspacePageForContextChange(surface, cause)...)
	if options.ForceClearReviewState {
		surface.ReviewSession = nil
	} else {
		clearIdleReviewSession(surface)
	}
	return events
}

func (s *Service) pathPickerOwnedByActiveTargetPicker(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	record := s.activePathPicker(surface)
	flow := s.activeOwnerCardFlow(surface)
	if record == nil || flow == nil || flow.Kind != ownerCardFlowKindTargetPicker {
		return false
	}
	return strings.TrimSpace(record.OwnerFlowID) != "" && strings.TrimSpace(record.OwnerFlowID) == strings.TrimSpace(flow.FlowID)
}

func (s *Service) sealPathPickerForContextChange(surface *state.SurfaceConsoleRecord, cause string) []eventcontract.Event {
	record := s.activePathPicker(surface)
	if surface == nil || record == nil {
		return nil
	}
	if messageID := strings.TrimSpace(s.pathPickerMessageID(surface, record.PickerID)); messageID != "" {
		record.MessageID = messageID
		return s.finishPathPickerWithStatus(
			surface,
			record,
			frontstagecontract.PhaseFailed,
			"路径选择器已失效",
			overlayInvalidationText(cause, "这个路径选择器已失效，请重新发起。"),
			nil,
			"",
			false,
			nil,
		)
	}
	s.clearSurfacePathPicker(surface)
	return nil
}

func (s *Service) sealTargetPickerForContextChange(surface *state.SurfaceConsoleRecord, cause string) []eventcontract.Event {
	record := s.activeTargetPicker(surface)
	flow := s.activeOwnerCardFlow(surface)
	if surface == nil || record == nil {
		if flow != nil && flow.Kind == ownerCardFlowKindTargetPicker {
			s.clearSurfaceOwnerCardFlow(surface)
		}
		return nil
	}
	if flow == nil || flow.Kind != ownerCardFlowKindTargetPicker || strings.TrimSpace(flow.MessageID) == "" {
		s.clearTargetPickerRuntime(surface)
		return nil
	}
	return s.finishTargetPickerWithStage(
		surface,
		flow,
		record,
		control.FeishuTargetPickerStageFailed,
		"选择流程已失效",
		overlayInvalidationText(cause, "这张选择卡片已失效，请重新发送 /list、/use 或 /useall。"),
		false,
		nil,
	)
}

func (s *Service) sealThreadHistoryForContextChange(surface *state.SurfaceConsoleRecord, cause string) []eventcontract.Event {
	record := s.activeThreadHistory(surface)
	flow := s.activeOwnerCardFlow(surface)
	if surface == nil || record == nil {
		if flow != nil && flow.Kind == ownerCardFlowKindThreadHistory {
			s.clearSurfaceOwnerCardFlow(surface)
		}
		return nil
	}
	if flow == nil || flow.Kind != ownerCardFlowKindThreadHistory || strings.TrimSpace(flow.MessageID) == "" {
		s.clearThreadHistoryRuntime(surface)
		return nil
	}
	view := s.buildThreadHistoryErrorView(
		surface,
		s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)],
		flow,
		record,
		"history_expired",
		overlayInvalidationText(cause, "这张历史卡片已失效，请重新发送 /history。"),
	)
	s.clearThreadHistoryRuntime(surface)
	return []eventcontract.Event{s.threadHistoryViewEvent(surface, view, false, "")}
}

func (s *Service) sealReviewCommitPickerForContextChange(surface *state.SurfaceConsoleRecord, cause string) []eventcontract.Event {
	record := s.activeReviewPicker(surface)
	flow := s.activeOwnerCardFlow(surface)
	if surface == nil || record == nil {
		if flow != nil && flow.Kind == ownerCardFlowKindReviewPicker {
			s.clearSurfaceOwnerCardFlow(surface)
		}
		return nil
	}
	if flow == nil || flow.Kind != ownerCardFlowKindReviewPicker || strings.TrimSpace(flow.MessageID) == "" {
		s.clearReviewCommitPickerRuntime(surface)
		return nil
	}
	view := control.NormalizeFeishuPageView(control.FeishuPageView{
		CommandID:                     control.FeishuCommandReview,
		Title:                         "选择提交记录",
		MessageID:                     strings.TrimSpace(flow.MessageID),
		TrackingKey:                   strings.TrimSpace(flow.FlowID),
		Patchable:                     true,
		StatusKind:                    "info",
		StatusText:                    overlayInvalidationText(cause, "这张 commit 选择卡片已失效，请重新发送 `/review commit`。"),
		Sealed:                        true,
		SuppressDefaultRelatedButtons: true,
	})
	s.clearReviewCommitPickerRuntime(surface)
	return []eventcontract.Event{s.anchoredPageUpdateEvent(surface, view)}
}

func (s *Service) sealWorkspacePageForContextChange(surface *state.SurfaceConsoleRecord, cause string) []eventcontract.Event {
	record := s.activeWorkspacePage(surface)
	flow := s.activeOwnerCardFlow(surface)
	if surface == nil || record == nil {
		if flow != nil && flow.Kind == ownerCardFlowKindWorkspacePage {
			s.clearSurfaceOwnerCardFlow(surface)
		}
		return nil
	}
	if strings.TrimSpace(record.MessageID) == "" {
		s.clearWorkspacePageRuntime(surface)
		return nil
	}
	view := s.buildSealedWorkspacePageView(record, cause)
	s.clearWorkspacePageRuntime(surface)
	return []eventcontract.Event{s.anchoredPageUpdateEvent(surface, view)}
}

func (s *Service) buildSealedWorkspacePageView(record *activeWorkspacePageRecord, cause string) control.FeishuPageView {
	var view control.FeishuPageView
	switch strings.TrimSpace(record.CommandID) {
	case control.FeishuCommandWorkspaceNew:
		view = control.BuildFeishuWorkspaceNewPageView(record.FromMenu)
	default:
		view = control.BuildFeishuWorkspaceRootPageView(record.FromMenu)
	}
	view.MessageID = strings.TrimSpace(record.MessageID)
	view.TrackingKey = strings.TrimSpace(record.FlowID)
	view.Patchable = true
	view.Sealed = true
	view.Phase = frontstagecontract.PhaseFailed
	view.ActionPolicy = frontstagecontract.ActionPolicyReadOnly
	view.Interactive = false
	view.StatusKind = "info"
	view.StatusText = overlayInvalidationText(cause, "这张工作区页面已失效，请重新打开。")
	view.Sections = nil
	view.RelatedButtons = nil
	view.SuppressDefaultRelatedButtons = true
	return control.NormalizeFeishuPageView(view)
}

func (s *Service) anchoredPageUpdateEvent(surface *state.SurfaceConsoleRecord, view control.FeishuPageView) eventcontract.Event {
	view = control.NormalizeFeishuPageView(view)
	return surfaceEventFromPayload(
		surface,
		eventcontract.PagePayload{
			View:    view,
			Context: s.buildFeishuPageContextFromView(surface, view),
		},
		eventcontract.EventMeta{},
	)
}

func overlayInvalidationText(cause, detail string) string {
	cause = trimOverlaySentenceSuffix(cause)
	if cause == "" {
		cause = "当前工作目标已变化"
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return cause + "。"
	}
	return cause + "，" + detail
}

func trimOverlaySentenceSuffix(text string) string {
	text = strings.TrimSpace(text)
	for {
		switch {
		case strings.HasSuffix(text, "。"):
			text = strings.TrimSpace(strings.TrimSuffix(text, "。"))
		case strings.HasSuffix(text, "."):
			text = strings.TrimSpace(strings.TrimSuffix(text, "."))
		default:
			return text
		}
	}
}
