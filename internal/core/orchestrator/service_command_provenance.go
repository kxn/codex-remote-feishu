package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) rejectExpiredCommandEntry(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil || !action.IsCardAction() || action.LocalPageAction || !control.ActionRoutesThroughFeishuCommandCatalog(action) {
		return nil
	}
	if !control.HasStrictFeishuCommandCatalogProvenance(action) {
		return notice(surface, "command_entry_expired", "这个旧命令入口已失效，请重新发送命令或从当前菜单重新打开。")
	}
	if !control.MatchesFeishuCommandCatalogContext(action, s.buildCatalogContext(surface)) {
		return notice(surface, "command_entry_expired", "当前上下文已变化，这个旧命令入口已失效，请从当前菜单重新打开。")
	}
	return nil
}
