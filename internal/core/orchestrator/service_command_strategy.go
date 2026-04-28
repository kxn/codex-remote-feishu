package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) commandStrategyBlocked(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	strategy, ok := control.ResolveFeishuActionStrategy(s.buildCatalogContext(surface), action)
	if !ok || strategy.DispatchAllowed {
		return nil
	}
	text := strings.TrimSpace(strategy.Note)
	if text == "" {
		text = "当前 backend 暂不支持这个命令。"
	}
	return notice(surface, "command_rejected", text)
}
