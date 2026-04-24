package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func clearAutoWhipRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.AutoWhip = state.AutoWhipRuntimeRecord{}
}

func clearAutoContinueRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	enabled := surface.AutoContinue.Enabled
	surface.AutoContinue = state.AutoContinueRuntimeRecord{Enabled: enabled}
}

func parseProductMode(value string) (state.ProductMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "normal":
		return state.ProductModeNormal, true
	case "vscode", "vs-code", "vs_code":
		return state.ProductModeVSCode, true
	default:
		return "", false
	}
}

func parseSurfaceVerbosity(value string) (state.SurfaceVerbosity, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "quiet":
		return state.SurfaceVerbosityQuiet, true
	case "normal":
		return state.SurfaceVerbosityNormal, true
	case "verbose":
		return state.SurfaceVerbosityVerbose, true
	default:
		return "", false
	}
}

func parsePlanMode(value string) (state.PlanModeSetting, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on":
		return state.PlanModeSettingOn, true
	case "off":
		return state.PlanModeSettingOff, true
	default:
		return "", false
	}
}

func commandCardOwnsInlineResult(action control.Action) bool {
	return action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""
}

func actionCommandArgumentText(action control.Action) string {
	text := strings.TrimSpace(action.Text)
	if text == "" {
		return ""
	}
	idx := strings.IndexAny(text, " \t")
	if idx < 0 || idx+1 >= len(text) {
		return ""
	}
	return strings.TrimSpace(text[idx+1:])
}

func (s *Service) buildCommandConfigViewForAction(surface *state.SurfaceConsoleRecord, action control.Action, cardState control.FeishuCatalogConfigView) control.FeishuCatalogView {
	switch action.Kind {
	case control.ActionModeCommand:
		return s.buildModeCommandViewState(surface, cardState)
	case control.ActionAutoWhipCommand:
		return s.buildAutoWhipCommandViewState(surface, cardState)
	case control.ActionAutoContinueCommand:
		return s.buildAutoContinueCommandViewState(surface, cardState)
	case control.ActionReasoningCommand:
		return s.buildReasoningCommandViewState(surface, cardState)
	case control.ActionAccessCommand:
		return s.buildAccessCommandViewState(surface, cardState)
	case control.ActionPlanCommand:
		return s.buildPlanCommandViewState(surface, cardState)
	case control.ActionModelCommand:
		return s.buildModelCommandViewState(surface, cardState)
	case control.ActionVerboseCommand:
		return s.buildVerboseCommandViewState(surface, cardState)
	default:
		return control.FeishuCatalogView{}
	}
}

func (s *Service) inlineCommandCardEvents(surface *state.SurfaceConsoleRecord, action control.Action, cardState control.FeishuCatalogConfigView, extra ...eventcontract.Event) []eventcontract.Event {
	view := s.buildCommandConfigViewForAction(surface, action, cardState)
	events := []eventcontract.Event{s.configPageEventFromCatalogView(surface, view)}
	return append(events, extra...)
}

func (s *Service) handleModeCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	current := s.normalizeSurfaceProductMode(surface)
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []eventcontract.Event{s.configPageEventFromCatalogView(surface, s.buildModeCommandView(surface))}
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：/mode 查看当前状态；/mode normal；/mode vscode。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	target, ok := parseProductMode(parts[1])
	if !ok {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：/mode 查看当前状态；/mode normal；/mode vscode。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	if target == current {
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "info",
				StatusText: fmt.Sprintf("当前已处于 %s 模式。", target),
			})
		}
		return notice(surface, "surface_mode_current", fmt.Sprintf("当前已处于 %s 模式。", target))
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if s.surfaceHasLiveRemoteWork(surface) || s.surfaceNeedsDelayedDetach(surface, inst) {
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: "当前仍有执行中的 turn、派发中的请求或排队消息，暂时不能切换模式。请等待完成、/stop，或先 /detach。",
			})
		}
		return notice(surface, "surface_mode_busy", "当前仍有执行中的 turn、派发中的请求或排队消息，暂时不能切换模式。请等待完成、/stop，或先 /detach。")
	}

	events := s.discardDrafts(surface)
	pending := surface.PendingHeadless
	events = append(events, s.finalizeDetachedSurface(surface)...)
	if pending != nil {
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       pending.InstanceID,
				ThreadID:         pending.ThreadID,
				ThreadTitle:      pending.ThreadTitle,
				ThreadCWD:        pending.ThreadCWD,
			},
		})
	}
	surface.ProductMode = target
	if commandCardOwnsInlineResult(action) {
		statusText := fmt.Sprintf("已切换到 %s 模式。当前没有接管中的目标。", target)
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: statusText,
		}, events...)
	}
	return append(events, notice(surface, "surface_mode_switched", fmt.Sprintf("已切换到 %s 模式。当前没有接管中的目标。", target))...)
}

func (s *Service) handleAutoWhipCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []eventcontract.Event{s.configPageEventFromCatalogView(surface, s.buildAutoWhipCommandView(surface))}
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/autowhip` 查看当前状态；`/autowhip on`；`/autowhip off`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}

	switch strings.ToLower(parts[1]) {
	case "on", "enable", "enabled", "true":
		if surface.AutoWhip.Enabled {
			if commandCardOwnsInlineResult(action) {
				return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
					Sealed:     true,
					StatusKind: "info",
					StatusText: "当前飞书会话的 autowhip 已开启。",
				})
			}
			return notice(surface, "autowhip_enabled", "当前飞书会话的 AutoWhip 已开启。")
		}
		clearAutoWhipRuntime(surface)
		surface.AutoWhip.Enabled = true
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: "已开启当前飞书会话的 AutoWhip。服务重启后不会恢复之前的 AutoWhip 状态。",
			})
		}
		return notice(surface, "autowhip_enabled", "已开启当前飞书会话的 AutoWhip。服务重启后不会恢复之前的 AutoWhip 状态。")
	case "off", "disable", "disabled", "false":
		clearAutoWhipRuntime(surface)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: "已关闭当前飞书会话的 autowhip。",
			})
		}
		return notice(surface, "autowhip_disabled", "已关闭当前飞书会话的 AutoWhip。")
	default:
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/autowhip` 查看当前状态；`/autowhip on`；`/autowhip off`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
}

func (s *Service) handleAutoContinueCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []eventcontract.Event{s.configPageEventFromCatalogView(surface, s.buildAutoContinueCommandView(surface))}
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/autocontinue` 查看当前状态；`/autocontinue on`；`/autocontinue off`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}

	switch strings.ToLower(parts[1]) {
	case "on", "enable", "enabled", "true":
		if surface.AutoContinue.Enabled {
			if commandCardOwnsInlineResult(action) {
				return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
					Sealed:     true,
					StatusKind: "info",
					StatusText: "当前飞书会话的自动继续已开启。",
				})
			}
			return notice(surface, "autocontinue_enabled", "当前飞书会话的自动继续已开启。")
		}
		clearAutoContinueRuntime(surface)
		surface.AutoContinue.Enabled = true
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: "已开启当前飞书会话的自动继续。服务重启后不会恢复之前的自动继续状态。",
			})
		}
		return notice(surface, "autocontinue_enabled", "已开启当前飞书会话的自动继续。服务重启后不会恢复之前的自动继续状态。")
	case "off", "disable", "disabled", "false":
		surface.AutoContinue = state.AutoContinueRuntimeRecord{}
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: "已关闭当前飞书会话的自动继续。",
			})
		}
		return notice(surface, "autocontinue_disabled", "已关闭当前飞书会话的自动继续。")
	default:
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/autocontinue` 查看当前状态；`/autocontinue on`；`/autocontinue off`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
}

func (s *Service) handleVerboseCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []eventcontract.Event{s.configPageEventFromCatalogView(surface, s.buildVerboseCommandView(surface))}
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/verbose` 查看当前设置；`/verbose quiet`；`/verbose normal`；`/verbose verbose`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	target, ok := parseSurfaceVerbosity(parts[1])
	if !ok {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/verbose` 查看当前设置；`/verbose quiet`；`/verbose normal`；`/verbose verbose`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	current := state.NormalizeSurfaceVerbosity(surface.Verbosity)
	if target == current {
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "info",
				StatusText: fmt.Sprintf("当前飞书前端详细程度已经是 %s。", target),
			})
		}
		return notice(surface, "surface_verbose_current", fmt.Sprintf("当前飞书前端详细程度已经是 %s。", target))
	}
	surface.Verbosity = target
	if commandCardOwnsInlineResult(action) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: fmt.Sprintf("已将当前飞书会话的前端详细程度切换为 %s。", target),
		})
	}
	return notice(surface, "surface_verbose_updated", fmt.Sprintf("已将当前飞书会话的前端详细程度切换为 %s。", target))
}

func (s *Service) handlePlanCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []eventcontract.Event{s.configPageEventFromCatalogView(surface, s.buildPlanCommandView(surface))}
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/plan` 查看当前设置；`/plan on`；`/plan off`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	target, ok := parsePlanMode(parts[1])
	if !ok {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/plan` 查看当前设置；`/plan on`；`/plan off`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	current := state.NormalizePlanModeSetting(surface.PlanMode)
	if target == current {
		text := fmt.Sprintf("当前 Plan mode 已经是 %s。", target)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "info",
				StatusText: text,
			})
		}
		return notice(surface, "surface_plan_mode_current", text)
	}
	surface.PlanMode = target
	text := fmt.Sprintf("已将当前飞书会话的 Plan mode 切换为 %s。", target)
	if surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) != 0 {
		text += " 当前已在执行或排队的消息不受影响。"
	}
	if commandCardOwnsInlineResult(action) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: text,
		})
	}
	return notice(surface, "surface_plan_mode_updated", text)
}

func (s *Service) handleModelCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []eventcontract.Event{s.configPageEventFromCatalogView(surface, s.buildModelCommandView(surface))}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: s.notAttachedText(surface),
			})
		}
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if len(parts) == 2 && isClearCommand(parts[1]) {
		surface.PromptOverride.Model = ""
		surface.PromptOverride.ReasoningEffort = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: "已清除飞书临时模型覆盖。之后从飞书发送的消息将恢复使用底层真实配置。",
			})
		}
		return notice(surface, "surface_override_cleared", "已清除飞书临时模型覆盖。之后从飞书发送的消息将恢复使用底层真实配置。")
	}
	if len(parts) > 3 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/model` 查看当前配置；`/model <模型>`；`/model <模型> <推理强度>`；`/model clear`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	override := surface.PromptOverride
	override.Model = parts[1]
	if len(parts) == 3 {
		if !looksLikeReasoningEffort(parts[2]) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind:       "error",
				StatusText:       "推理强度建议使用 `low`、`medium`、`high` 或 `xhigh`。",
				FormDefaultValue: actionCommandArgumentText(action),
			})
		}
		override.ReasoningEffort = strings.ToLower(parts[2])
	}
	surface.PromptOverride = override
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	if commandCardOwnsInlineResult(action) {
		_ = summary
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: "已更新飞书临时模型覆盖。",
		})
	}
	return notice(surface, "surface_override_updated", formatOverrideNotice(summary, "已更新飞书临时模型覆盖。"))
}

func (s *Service) handleReasoningCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []eventcontract.Event{s.configPageEventFromCatalogView(surface, s.buildReasoningCommandView(surface))}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: s.notAttachedText(surface),
			})
		}
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if len(parts) == 2 && isClearCommand(parts[1]) {
		surface.PromptOverride.ReasoningEffort = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: "已清除飞书临时推理强度覆盖。",
			})
		}
		return notice(surface, "surface_override_reasoning_cleared", "已清除飞书临时推理强度覆盖。")
	}
	if len(parts) != 2 || !looksLikeReasoningEffort(parts[1]) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/reasoning` 查看当前配置；`/reasoning <推理强度>`；`/reasoning clear`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	surface.PromptOverride.ReasoningEffort = strings.ToLower(parts[1])
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	if commandCardOwnsInlineResult(action) {
		_ = summary
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: "已更新飞书临时推理强度覆盖。",
		})
	}
	return notice(surface, "surface_override_updated", formatOverrideNotice(summary, "已更新飞书临时推理强度覆盖。"))
}

func (s *Service) handleAccessCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []eventcontract.Event{s.configPageEventFromCatalogView(surface, s.buildAccessCommandView(surface))}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: s.notAttachedText(surface),
			})
		}
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/access` 查看当前配置；`/access full`；`/access confirm`；`/access clear`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	if isClearCommand(parts[1]) {
		surface.PromptOverride.AccessMode = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
		if commandCardOwnsInlineResult(action) {
			_ = summary
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: "已恢复飞书默认执行权限。",
			})
		}
		return notice(surface, "surface_access_reset", formatOverrideNotice(summary, "已恢复飞书默认执行权限。"))
	}
	mode := agentproto.NormalizeAccessMode(parts[1])
	if mode == "" {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "执行权限建议使用 `full` 或 `confirm`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	surface.PromptOverride.AccessMode = mode
	surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	if commandCardOwnsInlineResult(action) {
		_ = summary
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: "已更新飞书执行权限模式。",
		})
	}
	return notice(surface, "surface_access_updated", formatOverrideNotice(summary, "已更新飞书执行权限模式。"))
}
