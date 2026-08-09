package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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

func (s *Service) unknownSlashCommandBlocked(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil || action.Kind != control.ActionTextMessage || s.surfaceBackend(surface) != agentproto.BackendOpenCode {
		return nil
	}
	text := strings.TrimSpace(action.Text)
	if !strings.HasPrefix(text, "/") {
		return nil
	}
	if _, ok := control.ParseFeishuTextActionWithoutCatalog(text); ok {
		return nil
	}
	return notice(surface, "command_rejected", "OpenCode 当前不支持直接发送 `"+text+"` 这类未登记的 slash command。请使用 /help 查看当前支持的命令。")
}
