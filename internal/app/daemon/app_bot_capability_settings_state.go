package daemon

import (
	"log"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (a *App) configureBotCapabilitySettingsStateLocked(stateDir string) {
	path := botcapabilitysettings.StatePath(stateDir)
	a.botCapabilitySettingsState.persistedStoreRuntimeState = loadPersistedStore("bot capability settings", path, botcapabilitysettings.LoadStore)
	a.materializeBotCapabilitySettingsStateLocked()
}

func (a *App) materializeBotCapabilitySettingsStateLocked() {
	if a.botCapabilitySettingsState.store == nil {
		return
	}
	entries := a.botCapabilitySettingsState.store.Entries()
	records := make([]state.BotCapabilitySettingsRecord, 0, len(entries))
	for _, record := range entries {
		records = append(records, record)
	}
	a.service.MaterializeBotCapabilitySettings(records)
}

func (a *App) syncBotCapabilitySettingsStateLocked() {
	if a.legacyWorkRetired() {
		return
	}
	if !a.botCapabilitySettingsState.writable() || a.botCapabilitySettingsState.store == nil {
		return
	}
	for _, record := range a.service.BotCapabilitySettings() {
		if err := a.botCapabilitySettingsState.store.Put(record); err != nil {
			log.Printf("persist bot capability settings failed: gateway=%s err=%v", record.GatewayID, err)
		}
	}
}
