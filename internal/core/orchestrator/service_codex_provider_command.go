package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) resolveCodexProviderSelection(value string) (state.CodexProviderRecord, bool) {
	targetID := state.NormalizeCodexProviderID(value)
	for _, provider := range s.CodexProviders() {
		if strings.EqualFold(strings.TrimSpace(provider.ID), targetID) {
			return provider, true
		}
	}
	return state.CodexProviderRecord{}, false
}

func (s *Service) handleCodexProviderCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if !s.surfaceIsHeadless(surface) || s.surfaceBackend(surface) != agentproto.BackendCodex {
		text := "当前不在 Codex 模式，暂时不能切换 Codex Provider。请先 `/mode codex`。"
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: text,
			})
		}
		return notice(surface, "codex_provider_mode_required", text)
	}

	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return s.openConfigCommandPageForAction(surface, action)
	}
	if len(parts) != 2 {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "用法：`/codexprovider` 查看当前状态；`/codexprovider <provider-id>`。",
			FormDefaultValue: actionCommandArgumentText(action),
		})
	}

	target, ok := s.resolveCodexProviderSelection(parts[1])
	if !ok {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind:       "error",
			StatusText:       "找不到这个 Codex Provider。请从下拉里选择已有配置，或到管理页先创建。",
			FormDefaultValue: state.NormalizeCodexProviderID(parts[1]),
		})
	}

	currentProviderID := s.surfaceCodexProviderID(surface)
	currentWorkspaceKey := normalizeWorkspaceClaimKey(s.surfaceCurrentWorkspaceKey(surface))
	targetLabel := s.codexProviderDisplayName(target.ID)
	if target.ID == currentProviderID {
		text := fmt.Sprintf("当前已在使用 Codex Provider：%s。", targetLabel)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "info",
				StatusText: text,
			})
		}
		return notice(surface, "codex_provider_current", text)
	}

	if blocked := s.blockRouteMutation(surface); blocked != nil {
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
		text := "当前仍有执行中的 turn、派发中的请求、排队消息或工作区准备流程，暂时不能切换 Codex Provider。请等待完成、/stop，或先 /detach。"
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				StatusKind: "error",
				StatusText: text,
			})
		}
		return notice(surface, "codex_provider_busy", text)
	}

	continuation := s.buildHeadlessContractSwitchContinuation(surface, currentWorkspaceKey, agentproto.BackendCodex)
	events := s.discardDrafts(surface)
	events = s.queueHeadlessContractRestart(events, surface, continuation)
	events = append(events, s.finalizeDetachedSurface(surface)...)
	s.setSurfaceCodexProviderID(surface, target.ID)
	if currentWorkspaceKey == "" {
		text := fmt.Sprintf("已切换到 Codex Provider：%s。当前没有接管中的工作区。", targetLabel)
		if commandCardOwnsInlineResult(action) {
			return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
				Sealed:     true,
				StatusKind: "success",
				StatusText: text,
			}, events...)
		}
		return append(events, notice(surface, "codex_provider_switched", text)...)
	}

	s.transitionSurfaceRouteCore(surface, nil, surfaceRouteCoreState{WorkspaceKey: currentWorkspaceKey})
	resumeEvents := s.restartHeadlessContractContinuation(surface, continuation)
	statusText := fmt.Sprintf("已切换到 Codex Provider：%s。正在重新准备当前工作区。", targetLabel)
	if commandCardOwnsInlineResult(action) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: statusText,
		}, append(events, resumeEvents...)...)
	}
	events = append(events, notice(surface, "codex_provider_switched", statusText)...)
	return append(events, resumeEvents...)
}
