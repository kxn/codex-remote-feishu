package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const CodexTopicSettingPersistFailureText = "模型或推理强度设置未能保存，当前配置未改变，请稍后重试。"

// SetCodexTopicOverridePersister installs the durable write boundary. The caller
// serializes it with surface actions; failure must happen before queue mutation.
func (s *Service) SetCodexTopicOverridePersister(persist func(*state.SurfaceConsoleRecord) error) {
	s.persistCodexTopicOverride = persist
}

func (s *Service) setCodexTopicOverride(surface *state.SurfaceConsoleRecord, value state.CodexPromptOverrideRecord) []eventcontract.Event {
	value = state.NormalizeCodexPromptOverride(value)
	if surface.CodexPromptOverride == value && !surface.CodexPromptOverrideUpdatedAt.IsZero() {
		return nil
	}
	candidate := *surface
	candidate.CodexPromptOverride = value
	candidate.CodexPromptOverrideUpdatedAt = s.now().UTC()
	if s.persistCodexTopicOverride != nil {
		if err := s.persistCodexTopicOverride(&candidate); err != nil {
			return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: surface.GatewayID, SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{Code: "codex_topic_setting_persist_failed", Title: "设置失败", Text: CodexTopicSettingPersistFailureText, ThemeKey: "error"}}}
		}
	}
	surface.CodexPromptOverride = value
	surface.CodexPromptOverrideUpdatedAt = candidate.CodexPromptOverrideUpdatedAt
	return nil
}
