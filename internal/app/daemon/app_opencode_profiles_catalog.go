package daemon

import (
	"log"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func materializeOpenCodeProfileSummaries(cfg config.AppConfig) []state.OpenCodeProfileSummary {
	profiles := config.ListOpenCodeProfiles(cfg)
	records := make([]state.OpenCodeProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		status := ""
		available := true
		if !profile.BuiltIn {
			status = config.OpenCodeAPIProfileStatus(profile.OpenCodeAPIProfileSecretConfig)
			available = status == ""
		}
		records = append(records, state.OpenCodeProfileSummary{
			ID:         strings.TrimSpace(profile.ID),
			Revision:   profile.Revision,
			ETag:       state.OpenCodeProfileDefinitionETag(profile.ID, profile.Revision),
			Name:       strings.TrimSpace(profile.Name),
			BaseURL:    strings.TrimSpace(profile.BaseURL),
			Model:      strings.TrimSpace(profile.Model),
			StatusCode: strings.TrimSpace(status),
			Available:  available,
			BuiltIn:    profile.BuiltIn,
			Editable:   !profile.BuiltIn,
			Deletable:  !profile.BuiltIn,
			HasAPIKey:  strings.TrimSpace(profile.APIKey) != "",
		})
	}
	return records
}

func (a *App) syncOpenCodeProfilesCatalogLocked(cfg config.AppConfig) {
	if a == nil || a.service == nil {
		return
	}
	a.service.MaterializeOpenCodeProfiles(materializeOpenCodeProfileSummaries(cfg))
}

func (a *App) syncOpenCodeProfilesCatalogFromConfig() {
	if a == nil {
		return
	}
	loaded, err := a.loadAdminConfig()
	if err != nil {
		log.Printf("load opencode profiles catalog failed: err=%v", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.syncOpenCodeProfilesCatalogLocked(loaded.Config)
}
