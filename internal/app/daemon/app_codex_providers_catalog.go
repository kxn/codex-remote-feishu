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
