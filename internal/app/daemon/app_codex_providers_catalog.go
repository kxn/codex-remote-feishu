package daemon

import (
	"log"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func materializeCodexProviderRecords(cfg config.AppConfig) []state.CodexProviderRecord {
	providers := config.ListCodexProviders(cfg)
	records := make([]state.CodexProviderRecord, 0, len(providers))
	for _, provider := range providers {
		records = append(records, state.NormalizeCodexProviderRecord(state.CodexProviderRecord{
			ID:      strings.TrimSpace(provider.ID),
			Name:    strings.TrimSpace(provider.Name),
			BuiltIn: provider.BuiltIn,
		}))
	}
	return records
}

func (a *App) syncCodexProvidersCatalogLocked(cfg config.AppConfig) {
	if a == nil || a.service == nil {
		return
	}
	a.service.MaterializeCodexProviders(materializeCodexProviderRecords(cfg))
	a.service.MaterializeCodexProfiles(a.materializeCodexProfileSummariesLocked(cfg))
}

func (a *App) syncCodexProvidersCatalogFromConfig() {
	if a == nil {
		return
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		log.Printf("load codex providers catalog failed: err=%v", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncCodexProvidersCatalogLocked(loaded.Config)
}

func (a *App) materializeCodexProfileSummariesLocked(cfg config.AppConfig) []state.CodexProfileSummary {
	profiles := []state.CodexProfileSummary{{
		ID:              state.NativeCodexProfileID,
		Kind:            state.CodexProfileKindNative,
		Name:            "本机默认",
		Available:       true,
		ContextEditable: true,
	}}
	if oauth, ok := a.codexOAuthProfileState.current(); ok {
		profiles = append(profiles, codexOAuthProfileSummary(oauth, state.ProfileContextPreference{}))
	}
	for _, record := range config.NormalizeCodexAPIProfileRecords(cfg.Codex.Profiles) {
		current, ok := config.CurrentCodexAPIProfile(record)
		if !ok {
			continue
		}
		profiles = append(profiles, codexAPIProfileSummary(current, state.ProfileContextPreference{}))
	}
	return profiles
}

func codexOAuthProfileSummary(oauth state.CodexOAuthProfileState, preference state.ProfileContextPreference) state.CodexProfileSummary {
	return state.CodexProfileSummary{
		ID:                state.OAuthCodexProfileID,
		Revision:          oauth.Revision,
		Kind:              state.CodexProfileKindOAuth,
		Name:              "ChatGPT 登录",
		StatusCode:        strings.TrimSpace(firstNonEmpty(oauth.AvailabilityCode, oauth.LastProbeErrorCode, oauth.Status)),
		Available:         strings.TrimSpace(oauth.Status) == "detected" && strings.TrimSpace(oauth.AvailabilityCode) == "",
		ContextEditable:   true,
		ContextPreference: preference,
	}
}
