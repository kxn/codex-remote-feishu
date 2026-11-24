package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// refreshPrivateBotSettings reads under the config owner lock, releases it,
// then projects under the processor lock guarded by an atomic revision check.
func (a *App) refreshPrivateBotSettings(ctx context.Context, p *privatechat.Processor, o privatechat.Owner, generation int64) error {
	if p == nil {
		return privatechat.ErrJournalState
	}
	if err := a.refreshPrivateProfileCatalog(ctx, p); err != nil {
		return err
	}
	snapshot, err := a.readPrivateBotSettings(o.GatewayID, o.AppID)
	if err != nil {
		return err
	}
	if err = p.RefreshBotSettings(ctx, o, generation, snapshot.Record, func() bool { return a.privateSettingsRevision.Load() == snapshot.Revision }); err != nil {
		return err
	}
	a.privateSettingsCache.Store(o.GatewayID, privateBotSettingsCacheEntry{Owner: o, Snapshot: snapshot})
	return nil
}

type privateBotSettingsCacheEntry struct {
	Owner    privatechat.Owner
	Snapshot privateBotSettingsSnapshot
}

var errPrivateBotSettingsConflict = errors.New("bot settings changed")

// privateBotSettingsSnapshot is a cache token, not a second settings source.
// The existing config owner and botcapabilitysettings.Store remain authority.
type privateBotSettingsSnapshot struct {
	Record      state.BotCapabilitySettingsRecord
	Fingerprint string
	Revision    uint64
}

func (a *App) privateSettingsBotConfigured(cfg config.AppConfig, gateway, appID string) bool {
	for _, app := range a.runtimeGatewayApps(cfg) {
		if app.GatewayID == gateway && app.AppID == appID && app.Enabled {
			return true
		}
	}
	return false
}

func privateSettingsFingerprint(record state.BotCapabilitySettingsRecord, profileFingerprint string) string {
	raw, _ := json.Marshal(struct {
		Record  state.BotCapabilitySettingsRecord
		Profile string
	}{record, profileFingerprint})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// A broken backend profile must remain editable. Freeze the full configuration
// identity for CAS without exposing its contents, while launch still validates
// and reports the actual profile error.
func privateSettingsResolutionFingerprint(record state.BotCapabilitySettingsRecord, cfg config.AppConfig, resolved resolvedWorkBotSettings) string {
	if resolved.err == nil {
		return privateSettingsFingerprint(record, resolved.fingerprint)
	}
	raw, _ := json.Marshal(cfg)
	sum := sha256.Sum256(raw)
	return privateSettingsFingerprint(record, "unavailable:"+hex.EncodeToString(sum[:]))
}

// readPrivateBotSettings must be called outside the private Processor lock.
func (a *App) readPrivateBotSettings(gateway, appID string) (privateBotSettingsSnapshot, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return privateBotSettingsSnapshot{}, err
	}
	if !a.privateSettingsBotConfigured(loaded.Config, gateway, appID) {
		return privateBotSettingsSnapshot{}, fmt.Errorf("private bot identity changed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.botCapabilitySettingsState.writable() || a.botCapabilitySettingsState.store == nil {
		return privateBotSettingsSnapshot{}, fmt.Errorf("bot settings unavailable")
	}
	record := a.workBotSettingsLocked(gateway)
	resolved := a.resolveWorkBotSettings(record, loaded.Config)
	return privateBotSettingsSnapshot{Record: record, Fingerprint: privateSettingsResolutionFingerprint(record, loaded.Config, resolved), Revision: a.privateSettingsRevision.Load()}, nil
}

// compareAndSwapPrivateBotSettings is serialized with team settings writes by
// the existing owner lock. It never calls the private processor under a.mu.
func (a *App) compareAndSwapPrivateBotSettings(gateway, appID, principal, expected string, next state.BotCapabilitySettingsRecord) (privateBotSettingsSnapshot, error) {
	a.profileReferenceGate.RLock()
	defer a.profileReferenceGate.RUnlock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return privateBotSettingsSnapshot{}, err
	}
	if !a.privateSettingsBotConfigured(loaded.Config, gateway, appID) {
		return privateBotSettingsSnapshot{}, fmt.Errorf("private bot identity changed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.botCapabilitySettingsState.writable() || a.botCapabilitySettingsState.store == nil {
		return privateBotSettingsSnapshot{}, fmt.Errorf("bot settings unavailable")
	}
	current := a.workBotSettingsLocked(gateway)
	resolved := a.resolveWorkBotSettings(current, loaded.Config)
	if next.GatewayID != gateway {
		return privateBotSettingsSnapshot{}, fmt.Errorf("bot settings gateway mismatch")
	}
	if expected == "" {
		return privateBotSettingsSnapshot{}, fmt.Errorf("bot settings expectation missing")
	}
	if privateSettingsResolutionFingerprint(current, loaded.Config, resolved) != expected {
		// A crash after Store.Put but before receipt can be reconciled only
		// when the authoritative record already equals this exact intent.
		wanted, ok := state.NormalizeBotCapabilitySettingsRecord(next)
		if !ok {
			return privateBotSettingsSnapshot{}, fmt.Errorf("invalid bot settings")
		}
		wanted.UpdatedBy, wanted.UpdatedAt = principal, time.Time{}
		actual := current
		actual.UpdatedAt = time.Time{}
		actualJSON, _ := json.Marshal(actual)
		wantedJSON, _ := json.Marshal(wanted)
		if string(actualJSON) != string(wantedJSON) {
			return privateBotSettingsSnapshot{}, errPrivateBotSettingsConflict
		}
		return privateBotSettingsSnapshot{Record: current, Fingerprint: privateSettingsResolutionFingerprint(current, loaded.Config, resolved), Revision: a.privateSettingsRevision.Load()}, nil
	}
	next.UpdatedBy, next.UpdatedAt = principal, time.Now().UTC()
	next, ok := state.NormalizeBotCapabilitySettingsRecord(next)
	if !ok {
		return privateBotSettingsSnapshot{}, fmt.Errorf("invalid bot settings")
	}
	resolved = a.resolveWorkBotSettings(next, loaded.Config)
	if resolved.err != nil {
		return privateBotSettingsSnapshot{}, resolved.err
	}
	if err := a.botCapabilitySettingsState.store.Put(next); err != nil {
		return privateBotSettingsSnapshot{}, err
	}
	revision := a.privateSettingsRevision.Add(1)
	return privateBotSettingsSnapshot{Record: next, Fingerprint: privateSettingsFingerprint(next, resolved.fingerprint), Revision: revision}, nil
}
