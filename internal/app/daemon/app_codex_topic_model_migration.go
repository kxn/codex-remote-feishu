package daemon

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

func migrateLegacyCodexTopicOverrides(surfaces *surfaceresume.Store, bots *botcapabilitysettings.Store) error {
	if surfaces == nil {
		return errors.New("surface resume store is unavailable")
	}
	if bots == nil {
		return errors.New("bot capability settings store is unavailable")
	}

	type legacyOverride struct {
		model     string
		reasoning string
	}
	botEntries := bots.Entries()
	affected := make(map[string]legacyOverride)
	for key, record := range botEntries {
		if state.BotCapabilitySettingsContract(record).Backend != agentproto.BackendCodex {
			continue
		}
		override := state.NormalizeCodexPromptOverride(state.CodexPromptOverrideRecord{
			Model:           record.PromptOverride.Model,
			ReasoningEffort: record.PromptOverride.ReasoningEffort,
		})
		if override.Model == "" && override.ReasoningEffort == "" {
			continue
		}
		gatewayID := strings.TrimSpace(record.GatewayID)
		affected[gatewayID] = legacyOverride{model: override.Model, reasoning: override.ReasoningEffort}
		record.PromptOverride.Model = ""
		record.PromptOverride.ReasoningEffort = ""
		botEntries[key] = record
	}
	if len(affected) == 0 {
		return nil
	}

	surfaceEntries := surfaces.Entries()
	for key, entry := range surfaceEntries {
		ref, ok := feishuidentity.ParseSurfaceRef(entry.SurfaceSessionID)
		if !ok {
			continue
		}
		entryGatewayID := strings.TrimSpace(entry.GatewayID)
		if entryGatewayID != "" && entryGatewayID != ref.GatewayID {
			continue
		}
		override, ok := affected[ref.GatewayID]
		if !ok {
			continue
		}
		if !entry.CodexPromptOverrideUpdatedAt.IsZero() {
			continue
		}
		if strings.TrimSpace(entry.CodexModelOverride) == "" {
			entry.CodexModelOverride = override.model
		}
		if strings.TrimSpace(entry.CodexReasoningEffortOverride) == "" {
			entry.CodexReasoningEffortOverride = override.reasoning
		}
		entry.CodexPromptOverrideUpdatedAt = time.Now().UTC()
		surfaceEntries[key] = entry
	}
	if err := surfaces.ReplaceAll(surfaceEntries); err != nil {
		return fmt.Errorf("persist surface phase of legacy Codex topic override migration: %w", err)
	}

	records := make([]state.BotCapabilitySettingsRecord, 0, len(botEntries))
	for _, record := range botEntries {
		records = append(records, record)
	}
	if err := bots.ReplaceAll(records); err != nil {
		return fmt.Errorf("persist bot phase of legacy Codex topic override migration: %w", err)
	}
	return nil
}

func (a *App) runLegacyCodexTopicOverrideMigrationLocked() error {
	var err error
	switch {
	case !a.surfaceResumeRuntime.writable():
		err = persistedStateWriteError("surface resume", a.surfaceResumeRuntime.persistedStoreRuntimeState)
	case a.surfaceResumeRuntime.store == nil:
		err = errors.New("surface resume store is unavailable for legacy Codex topic override migration")
	case !a.botCapabilitySettingsState.writable():
		err = persistedStateWriteError("bot capability settings", a.botCapabilitySettingsState.persistedStoreRuntimeState)
	case a.botCapabilitySettingsState.store == nil:
		err = errors.New("bot capability settings store is unavailable for legacy Codex topic override migration")
	default:
		err = migrateLegacyCodexTopicOverrides(a.surfaceResumeRuntime.store, a.botCapabilitySettingsState.store)
	}
	a.surfaceResumeRuntime.codexTopicModelMigrationErr = err
	if err == nil {
		a.materializeBotCapabilitySettingsStateLocked()
	}
	return err
}

func (a *App) legacyCodexTopicMigrationBlocksEmptySurfaceSyncLocked() bool {
	// SetHeadlessRuntime performs one empty startup sync after configuration;
	// keep it from erasing the fixed snapshot when migration failed.
	return a.surfaceResumeRuntime.codexTopicModelMigrationErr != nil && len(a.service.Surfaces()) == 0
}
