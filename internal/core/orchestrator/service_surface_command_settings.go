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

type surfaceModeSelection struct {
	ProductMode state.ProductMode
	Backend     agentproto.Backend
}

func parseSurfaceModeSelection(value string) (surfaceModeSelection, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "normal", "codex":
		return surfaceModeSelection{
			ProductMode: state.ProductModeNormal,
			Backend:     agentproto.BackendCodex,
		}, true
	case "claude":
		return surfaceModeSelection{
			ProductMode: state.ProductModeNormal,
			Backend:     agentproto.BackendClaude,
		}, true
	case "vscode", "vs-code", "vs_code":
		return surfaceModeSelection{
			ProductMode: state.ProductModeVSCode,
			Backend:     agentproto.BackendCodex,
		}, true
	default:
		return surfaceModeSelection{}, false
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

func (s *Service) resolveClaudeProfileSelection(value string) (state.ClaudeProfileRecord, bool) {
	targetID := state.NormalizeClaudeProfileID(value)
	for _, profile := range s.ClaudeProfiles() {
		if strings.EqualFold(strings.TrimSpace(profile.ID), targetID) {
			return profile, true
		}
	}
	return state.ClaudeProfileRecord{}, false
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
	flow, ok := control.ResolveFeishuConfigFlowDefinitionFromAction(action)
	if !ok {
		return control.FeishuCatalogView{}
	}
	return s.buildConfigCommandViewState(surface, flow, mergeConfigCardStateFromAction(flow, action, cardState))
}

func (s *Service) openConfigCommandPageForAction(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	view := s.buildCommandConfigViewForAction(surface, action, control.FeishuCatalogConfigView{})
	if view.Config == nil {
		return nil
	}
	return []eventcontract.Event{s.configPageEventFromCatalogView(surface, view)}
}

func (s *Service) inlineCommandCardEvents(surface *state.SurfaceConsoleRecord, action control.Action, cardState control.FeishuCatalogConfigView, extra ...eventcontract.Event) []eventcontract.Event {
	view := s.buildCommandConfigViewForAction(surface, action, cardState)
	events := []eventcontract.Event{s.configPageEventFromCatalogView(surface, view)}
	return append(events, extra...)
}

func (s *Service) handleModeCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	currentMode := s.normalizeSurfaceProductMode(surface)
	currentBackend := s.surfaceBackend(surface)
	currentAlias := state.SurfaceModeAlias(currentMode, currentBackend)
	currentWorkspaceKey := normalizeWorkspaceClaimKey(s.surfaceCurrentWorkspaceKey(surface))
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return s.openConfigCommandPageForAction(surface, action)
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：/mode 查看当前状态；/mode normal|codex|claude|vscode。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	target, ok := parseSurfaceModeSelection(parts[1])
	if !ok {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：/mode 查看当前状态；/mode normal|codex|claude|vscode。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}
	targetAlias := state.SurfaceModeAlias(target.ProductMode, target.Backend)
	if target.ProductMode == currentMode && target.Backend == currentBackend {
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "info",
				StatusText: fmt.Sprintf("当前已处于 %s 模式。", currentAlias),
			})
		}
		return notice(surface, "surface_mode_current", fmt.Sprintf("当前已处于 %s 模式。", currentAlias))
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
	surface.ProductMode = target.ProductMode
	surface.Backend = state.NormalizeSurfaceBackend(target.ProductMode, target.Backend)
	if target.ProductMode == state.ProductModeNormal && target.Backend == agentproto.BackendClaude {
		_ = s.surfaceClaudeProfileID(surface)
	}
	if currentWorkspaceKey != "" && target.ProductMode == state.ProductModeNormal {
		surface.ClaimedWorkspaceKey = currentWorkspaceKey
	}
	if target.ProductMode == state.ProductModeNormal && target.Backend == agentproto.BackendClaude && currentWorkspaceKey != "" {
		resumeEvents := s.resumeWorkspaceAfterClaudeModeSwitch(surface, currentWorkspaceKey)
		if commandCardOwnsInlineResult(action) {
			statusText := fmt.Sprintf("已切换到 %s 模式。正在准备当前工作区。", targetAlias)
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: statusText,
			}, append([]eventcontract.Event{}, resumeEvents...)...)
		}
		events = append(events, notice(surface, "surface_mode_switched", fmt.Sprintf("已切换到 %s 模式。正在准备当前工作区。", targetAlias))...)
		return append(events, resumeEvents...)
	}
	if commandCardOwnsInlineResult(action) {
		statusText := fmt.Sprintf("已切换到 %s 模式。当前没有接管中的目标。", targetAlias)
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: statusText,
		}, events...)
	}
	return append(events, notice(surface, "surface_mode_switched", fmt.Sprintf("已切换到 %s 模式。当前没有接管中的目标。", targetAlias))...)
}

func (s *Service) resumeWorkspaceAfterClaudeModeSwitch(surface *state.SurfaceConsoleRecord, workspaceKey string) []eventcontract.Event {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if surface == nil || workspaceKey == "" {
		return nil
	}
	if inst := s.resolveWorkspaceAttachInstance(surface, workspaceKey); inst != nil {
		return s.attachWorkspace(surface, workspaceKey)
	}
	return s.startFreshWorkspaceHeadlessWithOptions(surface, workspaceKey, false)
}

func (s *Service) handleClaudeProfileCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal || s.surfaceBackend(surface) != agentproto.BackendClaude {
		text := "当前不在 Claude normal 模式，暂时不能切换 Claude 配置。请先 `/mode claude`。"
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: text,
			})
		}
		return notice(surface, "claude_profile_mode_required", text)
	}

	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return s.openConfigCommandPageForAction(surface, action)
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/claudeprofile` 查看当前状态；`/claudeprofile <profile-id>`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}

	target, ok := s.resolveClaudeProfileSelection(parts[1])
	if !ok {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "找不到这个 Claude 配置。请从下拉里选择已有配置，或到管理页先创建。",
			FormDefaultValue: state.NormalizeClaudeProfileID(parts[1]),
		})
	}

	currentProfileID := s.surfaceClaudeProfileID(surface)
	currentWorkspaceKey := normalizeWorkspaceClaimKey(s.surfaceCurrentWorkspaceKey(surface))
	targetLabel := s.claudeProfileDisplayName(target.ID)
	if target.ID == currentProfileID {
		text := fmt.Sprintf("当前已在使用 Claude 配置：%s。", targetLabel)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "info",
				StatusText: text,
			})
		}
		return notice(surface, "claude_profile_current", text)
	}

	if blocked := s.blockRouteMutationForRequestState(surface); blocked != nil {
		if commandCardOwnsInlineResult(action) {
			text := ""
			if len(blocked) > 0 && blocked[0].Notice != nil {
				text = blocked[0].Notice.Text
			}
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: text,
			})
		}
		return blocked
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	if surface.PendingHeadless != nil || s.surfaceHasLiveRemoteWork(surface) || s.surfaceNeedsDelayedDetach(surface, inst) {
		text := "当前仍有执行中的 turn、派发中的请求、排队消息或工作区准备流程，暂时不能切换 Claude 配置。请等待完成、/stop，或先 /detach。"
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: text,
			})
		}
		return notice(surface, "claude_profile_busy", text)
	}

	prepareNewThread := surface.RouteMode == state.RouteModeNewThreadReady
	events := s.discardDrafts(surface)
	events = append(events, s.finalizeDetachedSurface(surface)...)
	s.setSurfaceClaudeProfileID(surface, target.ID)
	if currentWorkspaceKey == "" {
		surface.PromptOverride = state.ModelConfigRecord{}
		surface.PlanMode = state.PlanModeSettingOff
		text := fmt.Sprintf("已切换到 Claude 配置：%s。当前没有接管中的工作区。", targetLabel)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: text,
			}, events...)
		}
		return append(events, notice(surface, "claude_profile_switched", text)...)
	}

	surface.ClaimedWorkspaceKey = currentWorkspaceKey
	s.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)
	resumeEvents := s.startFreshWorkspaceHeadlessWithOptions(surface, currentWorkspaceKey, prepareNewThread)
	statusText := fmt.Sprintf("已切换到 Claude 配置：%s。正在重新准备当前工作区。", targetLabel)
	if commandCardOwnsInlineResult(action) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: statusText,
		}, append(events, resumeEvents...)...)
	}
	events = append(events, notice(surface, "claude_profile_switched", statusText)...)
	return append(events, resumeEvents...)
}

func (s *Service) handleAutoWhipCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return s.openConfigCommandPageForAction(surface, action)
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
		return s.openConfigCommandPageForAction(surface, action)
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
		return s.openConfigCommandPageForAction(surface, action)
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
		return s.openConfigCommandPageForAction(surface, action)
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
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
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
		return s.openConfigCommandPageForAction(surface, action)
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
		s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
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
		return s.openConfigCommandPageForAction(surface, action)
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
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
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
		return s.openConfigCommandPageForAction(surface, action)
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
		s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
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
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
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
