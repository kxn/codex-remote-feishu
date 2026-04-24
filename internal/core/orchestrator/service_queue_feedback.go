package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const remoteTurnAcceptedNoticeCode = "remote_turn_accepted"

func (s *Service) acceptedQueueFeedbackEvent(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, queuePosition int) []eventcontract.Event {
	if surface == nil || item == nil || item.SourceKind != state.QueueItemSourceUser {
		return nil
	}
	if state.NormalizeSurfaceVerbosity(surface.Verbosity) == state.SurfaceVerbosityQuiet {
		return nil
	}
	sourceMessageID := strings.TrimSpace(item.SourceMessageID)
	if sourceMessageID == "" {
		return nil
	}
	line := acceptedQueueFeedbackLine(surface, s.root.Instances[surface.AttachedInstanceID], item, queuePosition)
	if line == "" {
		return nil
	}
	return []eventcontract.Event{{
		Kind:                 eventcontract.KindNotice,
		GatewayID:            surface.GatewayID,
		SurfaceSessionID:     surface.SurfaceSessionID,
		SourceMessageID:      sourceMessageID,
		SourceMessagePreview: strings.TrimSpace(item.SourceMessagePreview),
		Notice: &control.Notice{
			Code:     remoteTurnAcceptedNoticeCode,
			Title:    "已接收",
			ThemeKey: "info",
			Sections: []control.FeishuCardTextSection{{
				Label: "执行状态",
				Lines: []string{line},
			}},
		},
		Meta: eventcontract.EventMeta{
			Semantics: eventcontract.DeliverySemantics{
				VisibilityClass:        eventcontract.VisibilityClassProgressText,
				HandoffClass:           eventcontract.HandoffClassProcessDetail,
				FirstResultDisposition: eventcontract.FirstResultDispositionKeep,
				OwnerCardDisposition:   eventcontract.OwnerCardDispositionKeep,
			},
			MessageDelivery: eventcontract.ReplyThreadAppendOnlyDelivery(),
		},
	}}
}

func acceptedQueueFeedbackLine(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, item *state.QueueItemRecord, queuePosition int) string {
	if surface == nil || item == nil {
		return ""
	}
	switch item.Status {
	case state.QueueItemDispatching, state.QueueItemRunning:
		return "正在发送到本地 Codex 并开始处理。"
	case state.QueueItemQueued:
		if inst == nil || !inst.Online {
			return "当前实例离线，消息已排队；恢复后会自动继续。"
		}
		switch surface.DispatchMode {
		case state.DispatchModePausedForLocal:
			return "当前本地 VS Code 占用，消息已排队。"
		case state.DispatchModeHandoffWait:
			return "等待本地 turn 交接，消息已排队。"
		}
		if surface.ActiveQueueItemID != "" {
			if queuePosition > 0 {
				return fmt.Sprintf("当前已有任务执行中，消息已排队（第 %d 位）。", queuePosition)
			}
			return "当前已有任务执行中，消息已排队。"
		}
		if queuePosition > 0 {
			return fmt.Sprintf("消息已排队（第 %d 位）。", queuePosition)
		}
		return "消息已排队，等待处理。"
	default:
		return ""
	}
}
