package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	detourTriggerForkText  = "[什么？]"
	detourTriggerBlankText = "[耸肩摊手]"

	detourForkLabel  = "临时会话 · 分支"
	detourBlankLabel = "临时会话 · 空白"

	detourAmbiguousTriggerText   = "我不知道应该选哪一个"
	detourForkRequiresThreadText = "当前没有可分支的会话，请先 /use 选择一个会话。"
	detourEmptyPromptText        = "请在 detour 标记之外补充消息内容。"
	detourReturnNoticeText       = "临时会话已结束，已切回原会话。"
)

type detourDirective struct {
	Triggered            bool
	CleanText            string
	ExecutionMode        agentproto.PromptExecutionMode
	SourceThreadID       string
	SurfaceBindingPolicy agentproto.SurfaceBindingPolicy
}

func (s *Service) resolveDetourDirective(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, text string) (detourDirective, string) {
	text = strings.TrimSpace(text)
	hasFork := strings.Contains(text, detourTriggerForkText)
	hasBlank := strings.Contains(text, detourTriggerBlankText)
	if !hasFork && !hasBlank {
		return detourDirective{CleanText: text}, ""
	}
	if hasFork && hasBlank {
		return detourDirective{}, detourAmbiguousTriggerText
	}
	directive := detourDirective{
		Triggered:            true,
		CleanText:            stripDetourTriggers(text),
		SurfaceBindingPolicy: agentproto.SurfaceBindingPolicyKeepSurfaceSelection,
	}
	if hasFork {
		sourceThreadID := s.currentDetourSourceThread(surface, inst)
		if sourceThreadID == "" {
			return detourDirective{}, detourForkRequiresThreadText
		}
		directive.ExecutionMode = agentproto.PromptExecutionModeForkEphemeral
		directive.SourceThreadID = sourceThreadID
		return directive, ""
	}
	directive.ExecutionMode = agentproto.PromptExecutionModeStartEphemeral
	return directive, ""
}

func stripDetourTriggers(text string) string {
	text = strings.ReplaceAll(text, detourTriggerForkText, "")
	text = strings.ReplaceAll(text, detourTriggerBlankText, "")
	return strings.TrimSpace(text)
}

func stripDetourInputs(inputs []agentproto.Input) []agentproto.Input {
	if len(inputs) == 0 {
		return nil
	}
	out := make([]agentproto.Input, 0, len(inputs))
	for _, input := range inputs {
		sanitized := input
		if sanitized.Type == agentproto.InputText {
			sanitized.Text = stripDetourTriggers(sanitized.Text)
			if strings.TrimSpace(sanitized.Text) == "" {
				continue
			}
		}
		out = append(out, sanitized)
	}
	return out
}

func (s *Service) currentDetourSourceThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	if surface == nil || inst == nil {
		return ""
	}
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if threadID == "" || !s.surfaceOwnsThread(surface, threadID) {
		return ""
	}
	if !threadVisible(inst.Threads[threadID]) {
		return ""
	}
	return threadID
}

func freezeDetourRoute(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord) (cwd string, routeMode state.RouteMode) {
	if surface == nil {
		return "", ""
	}
	routeMode = surface.RouteMode
	if strings.TrimSpace(surface.PreparedThreadCWD) != "" {
		return strings.TrimSpace(surface.PreparedThreadCWD), routeMode
	}
	if inst != nil {
		threadID := strings.TrimSpace(surface.SelectedThreadID)
		if threadID != "" {
			if thread := inst.Threads[threadID]; threadVisible(thread) && strings.TrimSpace(thread.CWD) != "" {
				return strings.TrimSpace(thread.CWD), routeMode
			}
		}
		if strings.TrimSpace(inst.WorkspaceRoot) != "" {
			return strings.TrimSpace(inst.WorkspaceRoot), routeMode
		}
	}
	return "", routeMode
}

func detourLabelForExecutionMode(mode agentproto.PromptExecutionMode) string {
	switch agentproto.NormalizePromptExecutionMode(mode) {
	case agentproto.PromptExecutionModeForkEphemeral:
		return detourForkLabel
	case agentproto.PromptExecutionModeStartEphemeral:
		return detourBlankLabel
	default:
		return ""
	}
}

func queuedItemDetourLabel(item *state.QueueItemRecord) string {
	if item == nil {
		return ""
	}
	return detourLabelForExecutionMode(item.FrozenExecutionMode)
}

func remoteBindingDetourLabel(binding *remoteTurnBinding) string {
	if binding == nil {
		return ""
	}
	return detourLabelForExecutionMode(binding.ExecutionMode)
}

func (s *Service) requestDetourLabel(record *state.RequestPromptRecord) string {
	if record == nil {
		return ""
	}
	return remoteBindingDetourLabel(s.lookupRemoteTurn(record.InstanceID, record.ThreadID, record.TurnID))
}

func (s *Service) detourReturnNoticeEvent(outcome *remoteTurnOutcome) []eventcontract.Event {
	if outcome == nil || outcome.Binding == nil || outcome.Surface == nil || outcome.Item == nil {
		return nil
	}
	if remoteBindingDetourLabel(outcome.Binding) == "" {
		return nil
	}
	sourceMessageID := strings.TrimSpace(firstNonEmpty(
		outcome.Binding.ReplyToMessageID,
		outcome.Item.ReplyToMessageID,
		outcome.Item.SourceMessageID,
	))
	meta := eventcontract.EventMeta{}
	if sourceMessageID != "" {
		meta.SourceMessageID = sourceMessageID
		meta.SourceMessagePreview = strings.TrimSpace(firstNonEmpty(
			outcome.Binding.ReplyToMessagePreview,
			outcome.Item.ReplyToMessagePreview,
			outcome.Item.SourceMessagePreview,
		))
		meta.MessageDelivery = eventcontract.ReplyThreadAppendOnlyDelivery()
	}
	return []eventcontract.Event{surfaceEventFromPayload(
		outcome.Surface,
		eventcontract.NoticePayload{Notice: control.Notice{
			Code:        "detour_returned",
			DetourLabel: remoteBindingDetourLabel(outcome.Binding),
			Text:        detourReturnNoticeText,
		}},
		meta,
	)}
}
