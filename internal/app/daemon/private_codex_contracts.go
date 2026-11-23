package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/profilecontextstate"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type privateCodexContractSnapshot struct {
	Version  string
	Profiles map[string]orchestrator.PrivateCodexContract
}

// Called with a.mu: only builds a host-owned immutable snapshot, never reads
// or mutates the private processor's core.
func (a *App) publishPrivateCodexContractsLocked(cfg config.AppConfig, prefs *profilecontextstate.Store, native state.CodexNativeConnectionEvidence, resolver codexprofile.RuntimeResolver) {
	ids := map[string]bool{config.CodexNativeProfileID: true, config.CodexOAuthProfileID: true}
	for _, profile := range cfg.Codex.Profiles {
		ids[profile.ID] = true
	}
	profiles := make(map[string]orchestrator.PrivateCodexContract, len(ids))
	for id := range ids {
		ref, ok := a.currentCodexAdmissionRefForProfileLocked(cfg, prefs, native, id)
		if !ok {
			profiles[id] = orchestrator.PrivateCodexContract{}
			continue
		}
		entry := orchestrator.PrivateCodexContract{Admission: state.NormalizeCodexAdmissionRef(&ref)}
		if resolved, err := resolver.Resolve(ref); err == nil {
			entry.Connection = state.CloneCodexConnectionContract(&resolved.Connection)
			entry.Thread = state.CloneCodexThreadPolicy(&resolved.Thread)
		}
		profiles[id] = entry
	}
	raw, _ := json.Marshal(profiles)
	sum := sha256.Sum256(raw)
	snapshot := &privateCodexContractSnapshot{Version: hex.EncodeToString(sum[:]), Profiles: profiles}
	a.privateCodexContracts.Store(snapshot)
}

// The host takes the snapshot read lock outside a.mu. The processor never
// acquires a.mu, preserving the single core writer and lock ordering.
func (a *App) reconcilePrivateCodexContracts(ctx context.Context, p *privatechat.Processor, o privatechat.Owner, generation int64, guard privatechat.ExecutionGuard) error {
	snapshot := a.privateCodexContracts.Load()
	if snapshot == nil {
		return nil
	}
	_, err := p.ApplyCodexContracts(ctx, o, generation, snapshot.Version, snapshot.Profiles, func() bool { return a.privateCodexContracts.Load() == snapshot }, guard)
	return err
}
