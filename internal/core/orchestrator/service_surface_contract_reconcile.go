package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// reconcileGatewayHeadlessSurfacesAfterContractChange 在 bot 级契约（Codex/Claude
// Profile 等）变化后，收敛同 gateway 下其他已 attach 的 headless surface：
// 空闲且不兼容的实例按新契约重启；忙的 surface 标记待收敛，由下一次交互入口处理。
// current 由调用方自己的切换流程处理，这里跳过。
func (s *Service) reconcileGatewayHeadlessSurfacesAfterContractChange(current *state.SurfaceConsoleRecord) []eventcontract.Event {
	if s == nil || s.root == nil || current == nil {
		return nil
	}
	gatewayKey := state.BotCapabilitySettingsKey(current.GatewayID)
	if gatewayKey == "" {
		return nil
	}
	var events []eventcontract.Event
	for _, surface := range s.root.Surfaces {
		if surface == nil || surface == current || state.BotCapabilitySettingsKey(surface.GatewayID) != gatewayKey {
			continue
		}
		if !s.surfaceIsHeadless(surface) {
			continue
		}
		events = append(events, s.reconcileHeadlessSurfaceContract(surface)...)
	}
	return events
}

func (s *Service) reconcileHeadlessSurfaceContract(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil || !isHeadlessInstance(inst) || !inst.Online {
		return nil
	}
	if s.surfaceInstanceCompatibleForAttach(surface, inst) {
		surface.ContractRefreshPending = false
		return nil
	}
	if surface.PendingHeadless != nil || s.surfaceHasLiveRemoteWork(surface) || s.surfaceNeedsDelayedDetach(surface, inst) {
		surface.ContractRefreshPending = true
		return nil
	}
	surface.ContractRefreshPending = false

	workspaceKey := s.surfaceCurrentWorkspaceKey(surface)
	if workspaceKey == "" {
		return nil
	}
	continuation := s.buildHeadlessContractSwitchContinuation(surface, workspaceKey, s.surfaceBackend(surface))
	if normalizeWorkspaceClaimKey(continuation.Attempt.WorkspaceKey) == "" && strings.TrimSpace(continuation.RestartInstanceID) == "" {
		return nil
	}
	events := s.queueHeadlessContractRestart(nil, surface, continuation)
	events = append(events, s.finalizeDetachedSurface(surface)...)
	return append(events, s.restartHeadlessContractContinuation(surface, continuation)...)
}
