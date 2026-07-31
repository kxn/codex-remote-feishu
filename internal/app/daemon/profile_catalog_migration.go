package daemon

import (
	"fmt"
	"log"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const profileCatalogMigrationVersion = 1

func (a *App) reconcileProfileCatalogMigration() {
	a.mu.Lock()
	adminConfigured := a.admin.loadConfig != nil
	a.mu.Unlock()
	if !adminConfigured {
		return
	}
	a.runProfileCatalogMigration()
}

func (a *App) runProfileCatalogMigration() {
	a.adminConfigMu.Lock()
	err := a.runProfileCatalogMigrationLocked()
	a.adminConfigMu.Unlock()
	a.mu.Lock()
	a.profileCatalogMigrationErr = err
	a.mu.Unlock()
	if err != nil {
		log.Printf("profile catalog migration entered read-only degraded mode: err=%v", err)
	}
}

func (a *App) runProfileCatalogMigrationLocked() error {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return fmt.Errorf("load migration config: %w", err)
	}
	if loaded.Config.Codex.ProfileCatalogMigrationVersion >= profileCatalogMigrationVersion {
		if len(loaded.Config.Codex.Providers) != 0 {
			return fmt.Errorf("migration marker conflicts with legacy codex providers")
		}
		if err := a.verifyProfileMigrationStoresWritable(); err != nil {
			return err
		}
		if err := a.verifyCommittedProfileCatalog(loaded.Config); err != nil {
			return err
		}
		a.mu.Lock()
		a.syncCodexProvidersCatalogLocked(loaded.Config)
		a.syncClaudeProfilesCatalogLocked(loaded.Config)
		a.mu.Unlock()
		return nil
	}
	if len(loaded.Config.Codex.Providers) > 0 && len(loaded.Config.Codex.Profiles) > 0 {
		return fmt.Errorf("legacy codex providers conflict with canonical profiles")
	}
	if err := a.verifyProfileMigrationStoresWritable(); err != nil {
		return err
	}

	planned, _, diagnostics := config.MigrateLegacyCodexProviders(loaded.Config)
	codexModes := map[string]string{config.CodexNativeProfileID: state.CodexContextModeDefault}
	knownRevisions := map[string]uint64{config.CodexNativeProfileID: 1}
	for _, record := range planned.Codex.Profiles {
		current, ok := config.CurrentCodexAPIProfile(record)
		if !ok {
			return fmt.Errorf("canonical profile %s has no current revision", record.ID)
		}
		codexModes[record.ID] = state.CodexContextModeDefault
		knownRevisions[record.ID] = current.Revision
	}
	claudeModes := map[string]string{config.ClaudeDefaultProfileID: state.ClaudeContextModeDefault}
	for index := range planned.Claude.Profiles {
		profile := &planned.Claude.Profiles[index]
		baseModel, extended := config.SplitClaudeExtendedContextSuffix(profile.Model)
		profile.Model = baseModel
		mode := state.ClaudeContextModeDefault
		if extended {
			mode = state.ClaudeContextModeExtended
		}
		claudeModes[profile.ID] = mode
	}
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return err
	}
	if err := preferenceStore.ApplyInitialPreferences(codexModes, claudeModes); err != nil {
		return fmt.Errorf("write initial profile preferences: %w", err)
	}

	selectionDiagnostics, err := a.migrateDurableProfileSelections(knownRevisions)
	if err != nil {
		return err
	}
	diagnostics = append(diagnostics, selectionDiagnostics...)
	planned.Version = currentConfigVersionValue()
	planned.Codex.ProfileCatalogMigrationVersion = profileCatalogMigrationVersion
	planned.Codex.MigrationDiagnostics = diagnostics
	if err := config.WriteAppConfig(loaded.Path, planned); err != nil {
		return fmt.Errorf("commit profile catalog migration: %w", err)
	}
	a.mu.Lock()
	a.syncCodexProvidersCatalogLocked(planned)
	a.syncClaudeProfilesCatalogLocked(planned)
	a.materializeBotCapabilitySettingsStateLocked()
	a.syncSurfaceResumeStateLocked(nil)
	a.mu.Unlock()
	return nil
}

func (a *App) profileCatalogMutationError() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.profileCatalogMigrationErr == nil {
		return nil
	}
	return fmt.Errorf("profile_catalog_degraded: %w", a.profileCatalogMigrationErr)
}

func (a *App) markProfileCatalogDegraded(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	a.profileCatalogMigrationErr = err
	a.mu.Unlock()
}

func (a *App) rollbackProfileConfig(path string, cfg config.AppConfig, cause error) error {
	if rollbackErr := config.WriteAppConfig(path, cfg); rollbackErr != nil {
		combined := fmt.Errorf("%v; config rollback failed: %w", cause, rollbackErr)
		a.markProfileCatalogDegraded(combined)
		return combined
	}
	return cause
}

func (a *App) verifyCommittedProfileCatalog(cfg config.AppConfig) error {
	store, err := a.profileContextPreferenceStore()
	if err != nil {
		return err
	}
	if _, ok := store.CodexCurrent(config.CodexNativeProfileID); !ok {
		return fmt.Errorf("native codex context preference is missing")
	}
	for _, record := range cfg.Codex.Profiles {
		if _, ok := config.CurrentCodexAPIProfile(record); !ok {
			return fmt.Errorf("codex profile current revision is missing for %s", record.ID)
		}
		if _, ok := store.CodexCurrent(record.ID); !ok {
			return fmt.Errorf("codex context preference is missing for %s", record.ID)
		}
	}
	if _, ok := store.ClaudeCurrent(config.ClaudeDefaultProfileID); !ok {
		return fmt.Errorf("default claude context preference is missing")
	}
	for _, profile := range cfg.Claude.Profiles {
		if _, ok := store.ClaudeCurrent(profile.ID); !ok {
			return fmt.Errorf("claude context preference is missing for %s", profile.ID)
		}
	}
	return nil
}

func (a *App) verifyProfileMigrationStoresWritable() error {
	if _, err := a.profileContextPreferenceStore(); err != nil {
		return err
	}
	if !a.botCapabilitySettingsState.writable() || a.botCapabilitySettingsState.store == nil {
		return fmt.Errorf("bot capability settings store is unavailable for profile migration")
	}
	if !a.surfaceResumeRuntime.writable() || a.surfaceResumeRuntime.store == nil {
		return fmt.Errorf("surface resume store is unavailable for profile migration")
	}
	return nil
}

func (a *App) migrateDurableProfileSelections(knownRevisions map[string]uint64) ([]config.CodexProfileMigrationDiagnostic, error) {
	diagnostics := make([]config.CodexProfileMigrationDiagnostic, 0)
	botEntries := a.botCapabilitySettingsState.store.Entries()
	botRecords := make([]state.BotCapabilitySettingsRecord, 0, len(botEntries))
	for _, record := range botEntries {
		record.CodexProfileID = selectedCodexProfileID(record.CodexProfileID, record.CodexProviderID)
		record.CodexProviderID = state.LegacyCodexProviderIDFromProfileID(record.CodexProfileID)
		if _, ok := knownRevisions[record.CodexProfileID]; !ok {
			diagnostics = append(diagnostics, config.CodexProfileMigrationDiagnostic{ProfileID: record.CodexProfileID, Code: "profile_not_found"})
		}
		botRecords = append(botRecords, record)
	}
	if err := a.botCapabilitySettingsState.store.ReplaceAll(botRecords); err != nil {
		return nil, fmt.Errorf("migrate bot profile selections: %w", err)
	}

	surfaceEntries := a.surfaceResumeRuntime.store.Entries()
	for key, entry := range surfaceEntries {
		entry.CodexProfileID = selectedCodexProfileID(entry.CodexProfileID, entry.CodexProviderID)
		entry.CodexProviderID = state.LegacyCodexProviderIDFromProfileID(entry.CodexProfileID)
		revision, known := knownRevisions[entry.CodexProfileID]
		if !known {
			diagnostics = append(diagnostics, config.CodexProfileMigrationDiagnostic{ProfileID: entry.CodexProfileID, Code: "profile_not_found"})
			entry.CodexAdmissionRef = nil
		}
		selectionConflict := entry.CodexProfileSelectionStatus == surfaceresume.CodexProfileSelectionStatusConflict
		if selectionConflict {
			diagnostics = append(diagnostics, config.CodexProfileMigrationDiagnostic{ProfileID: entry.CodexProfileID, Code: surfaceresume.CodexProfileSelectionStatusConflict})
			entry.CodexAdmissionRef = nil
		}
		if known && !selectionConflict && strings.TrimSpace(entry.ResumeThreadID) != "" && agentproto.NormalizeBackend(agentproto.Backend(entry.Backend)) == agentproto.BackendCodex {
			entry.CodexAdmissionRef = &state.CodexAdmissionRef{
				ProfileRef:           state.CodexProfileRef{ID: entry.CodexProfileID, Revision: revision},
				ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: entry.CodexProfileID, Revision: 1},
			}
		}
		surfaceEntries[key] = entry
	}
	if err := a.surfaceResumeRuntime.store.ReplaceAll(surfaceEntries); err != nil {
		return nil, fmt.Errorf("migrate surface profile selections: %w", err)
	}
	return diagnostics, nil
}

func selectedCodexProfileID(profileID, providerID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID != "" {
		return profileID
	}
	return state.CodexProfileIDFromLegacyProviderID(providerID)
}

func currentConfigVersionValue() int {
	return config.DefaultAppConfig().Version
}
