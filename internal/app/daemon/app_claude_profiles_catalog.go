package daemon

import (
	"log"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func materializeClaudeProfileRecords(cfg config.AppConfig, systemDefaultReasoningEffort string) []state.ClaudeProfileRecord {
	profiles := config.ListClaudeProfiles(cfg)
	records := make([]state.ClaudeProfileRecord, 0, len(profiles))
	systemDefaultReasoningEffort = state.NormalizeClaudeReasoningEffort(systemDefaultReasoningEffort)
	for _, profile := range profiles {
		reasoningEffort := strings.TrimSpace(profile.ReasoningEffort)
		if reasoningEffort == "" {
			reasoningEffort = systemDefaultReasoningEffort
		}
		records = append(records, state.NormalizeClaudeProfileRecord(state.ClaudeProfileRecord{
			ID:              strings.TrimSpace(profile.ID),
			Name:            strings.TrimSpace(profile.Name),
			ReasoningEffort: reasoningEffort,
			BuiltIn:         profile.BuiltIn,
		}))
	}
	return records
}

func (a *App) syncClaudeProfilesCatalogLocked(cfg config.AppConfig) {
	if a == nil || a.service == nil {
		return
	}
	a.service.MaterializeClaudeProfiles(materializeClaudeProfileRecords(cfg, claudeSystemReasoningEffortFromEnv(a.headlessRuntime.BaseEnv)))
}

func (a *App) syncClaudeProfilesCatalogFromConfig() {
	if a == nil {
		return
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		log.Printf("load claude profiles catalog failed: err=%v", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncClaudeProfilesCatalogLocked(loaded.Config)
}

func claudeSystemReasoningEffortFromEnv(env []string) string {
	effort := ""
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key != config.ClaudeEffortLevelEnv {
			continue
		}
		effort = state.NormalizeClaudeReasoningEffort(value)
	}
	return effort
}
