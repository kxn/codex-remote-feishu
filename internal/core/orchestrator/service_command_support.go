package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) commandSupportBlocked(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	support, ok := control.ResolveFeishuActionSupport(s.buildCatalogContext(surface), action)
	if !ok || support.DispatchAllowed {
		return nil
	}
	return s.commandSupportNotice(surface, support)
}

func (s *Service) commandSupportNotice(surface *state.SurfaceConsoleRecord, support control.FeishuCommandSupport) []eventcontract.Event {
	text := strings.TrimSpace(support.Note)
	if text == "" {
		text = "当前模式暂不支持这个命令。"
	}
	return notice(surface, "command_rejected", text)
}
