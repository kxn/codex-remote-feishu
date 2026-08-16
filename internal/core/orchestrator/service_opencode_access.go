package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) handleOpenCodeAccessCommand(surface *state.SurfaceConsoleRecord, action control.Action, parts []string) []eventcontract.Event {
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/access` 查看当前配置；`/access full`；`/access confirm`；`/access clear`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}

	target := ""
	clearing := isClearCommand(parts[1])
	if !clearing {
		target = state.NormalizeOpenCodeRuntimeAccessMode(parts[1])
		if target == "" {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind:       "error",
				StatusText:       "OpenCode 执行权限建议使用 `full` 或 `confirm`。",
				FormDefaultValue: actionCommandArgumentText(action),
			})
		}
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	currentWorkspaceKey := normalizeWorkspaceClaimKey(s.surfaceCurrentWorkspaceKey(surface))
	continuation := s.buildHeadlessContractSwitchContinuation(surface, currentWorkspaceKey, agentproto.BackendOpenCode)
	desired := s.headlessLaunchContractWithOverride(surface, state.ModelConfigRecord{AccessMode: target})
	restartNow := currentWorkspaceKey != "" &&
		surface.PendingHeadless == nil &&
		!s.surfaceHasLiveRemoteWork(surface) &&
		!s.surfaceNeedsDelayedDetach(surface, inst) &&
		!s.surfaceHasRouteMutationBlocker(surface)
	if restartNow && inst != nil && openCodeLaunchContractMatches(state.HeadlessLaunchContractFromInstance(inst), desired) {
		restartNow = false
	}

	var events []eventcontract.Event
	if restartNow {
		events = s.discardDrafts(surface)
		events = s.queueHeadlessContractRestart(events, surface, continuation)
		events = append(events, s.finalizeDetachedSurface(surface)...)
	}
	s.applyOpenCodeRuntimeAccessMode(surface, target)
	reconcileEvents := s.reconcileGatewayHeadlessSurfacesAfterContractChange(surface)

	statusText := "已更新 OpenCode 执行权限模式。"
	noticeCode := "opencode_access_updated"
	if clearing {
		statusText = "已恢复 OpenCode 默认执行权限。"
		noticeCode = "opencode_access_reset"
	}
	if restartNow {
		s.transitionSurfaceRouteCore(surface, nil, surfaceRouteCoreState{WorkspaceKey: currentWorkspaceKey})
		resumeEvents := s.restartHeadlessContractContinuation(surface, continuation)
		statusText += " 正在重新准备当前工作区。"
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: statusText,
			}, append(append(events, reconcileEvents...), resumeEvents...)...)
		}
		events = append(append(events, reconcileEvents...), notice(surface, noticeCode, statusText)...)
		return append(events, resumeEvents...)
	}
	if currentWorkspaceKey == "" {
		statusText += " 当前没有接管中的工作区。"
	} else if surface.PendingHeadless != nil || s.surfaceHasLiveRemoteWork(surface) || s.surfaceNeedsDelayedDetach(surface, inst) || s.surfaceHasRouteMutationBlocker(surface) {
		statusText += " 当前已在执行、排队或交互中的消息不受影响；下一条消息会在派发前按新权限准备 OpenCode。"
	}
	if commandCardOwnsInlineResult(action) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: statusText,
		}, append(events, reconcileEvents...)...)
	}
	return append(append(events, reconcileEvents...), notice(surface, noticeCode, statusText)...)
}

func (s *Service) applyOpenCodeRuntimeAccessMode(surface *state.SurfaceConsoleRecord, target string) {
	target = state.NormalizeOpenCodeRuntimeAccessMode(target)
	s.applySurfaceAccessModeChange(surface, func(override *state.ModelConfigRecord) {
		override.AccessMode = target
	})
}
