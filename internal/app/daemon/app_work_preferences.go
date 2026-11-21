package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamBotSettingsPort struct{ app *App }

func (p teamBotSettingsPort) facts(ctx context.Context, binding work.RoleBotBinding) (work.Role, []work.WorkerSession, error) {
	var role work.Role
	workers := []work.WorkerSession{}
	a := p.app
	if a == nil || a.teamRuntime == nil {
		return role, workers, worksettings.ErrUnavailable
	}
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		actual, err := tx.GetRoleBotBinding(binding.RoleID)
		if err != nil {
			return err
		}
		if actual != binding {
			return worksettings.ErrConflict
		}
		role, err = tx.GetRole(binding.RoleID)
		if err != nil {
			return err
		}

		return nil
	})
	return role, workers, err
}
func (p teamBotSettingsPort) Snapshot(ctx context.Context, binding work.RoleBotBinding) (worksettings.Snapshot, error) {
	var snapshot worksettings.Snapshot
	_, _, err := p.facts(ctx, binding)
	if err != nil {
		return snapshot, err
	}
	a := p.app
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return snapshot, err
	}
	apps := a.runtimeGatewayApps(loaded.Config)
	found := false
	for _, app := range apps {
		if app.GatewayID == binding.GatewayRef && app.AppID == binding.AppID && app.Enabled {
			found = true
		}
	}
	if !found {
		return snapshot, worksettings.ErrConflict
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	snapshot.Settings = a.workBotSettingsLocked(binding.GatewayRef)
	snapshot.Fingerprint = a.resolveWorkBotSettings(snapshot.Settings, loaded.Config).fingerprint
	for _, profile := range a.materializeCodexProfileSummariesLocked(loaded.Config) {
		model, reasoning, fixed := orchestrator.FixedCodexProfileSelection(profile)
		option := worksettings.Profile{ID: profile.ID, Name: profile.Name, Backend: agentproto.BackendCodex, Available: profile.Available}
		if fixed {
			option.FixedModel = model
			option.FixedReasoning = reasoning
		}
		snapshot.Profiles = append(snapshot.Profiles, option)
	}
	for _, profile := range config.ListClaudeProfiles(loaded.Config) {
		snapshot.Profiles = append(snapshot.Profiles, worksettings.Profile{ID: profile.ID, Name: profile.Name, Backend: agentproto.BackendClaude, Available: true})
	}
	for _, profile := range materializeOpenCodeProfileSummaries(loaded.Config) {
		snapshot.Profiles = append(snapshot.Profiles, worksettings.Profile{ID: profile.ID, Name: profile.Name, Backend: agentproto.BackendOpenCode, Available: profile.Available})
	}
	if snapshot.Catalog == nil {
		snapshot.CatalogUnavailable = "当前所选配置尚无匹配执行的可用模型列表；可选择已配置Profile，模型/推理选项需等待该配置的真实列表。"
	}
	return snapshot, nil
}
func teamSettingsProfile(record state.BotCapabilitySettingsRecord) string {
	switch record.Backend {
	case agentproto.BackendClaude:
		return state.NormalizeClaudeProfileID(record.ClaudeProfileID)
	case agentproto.BackendOpenCode:
		return state.NormalizeOpenCodeProfileID(record.OpenCodeProfileID)
	default:
		return state.NormalizeCodexProfileID(record.CodexProfileID)
	}
}
func (p teamBotSettingsPort) ApplyBot(ctx context.Context, binding work.RoleBotBinding, expected string, next state.BotCapabilitySettingsRecord, evidence worksettings.Evidence) (worksettings.Applied, error) {
	var applied worksettings.Applied
	a := p.app
	if a == nil || a.teamRuntime == nil {
		return applied, worksettings.ErrUnavailable
	}
	if evidence.PrincipalID == "" || evidence.Source.Validate() != nil || evidence.OperationID == "" || next.GatewayID != binding.GatewayRef {
		return applied, worksettings.ErrUnavailable
	}
	// Use the same configuration lock as launch and dispatch admission.
	// Keep Feishu gateway identity changes outside the save/projection pair.
	a.feishuApplyMu.Lock()
	defer a.feishuApplyMu.Unlock()
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	if _, _, err := p.facts(ctx, binding); err != nil {
		return applied, err
	}
	var err error
	applied.Fingerprint, err = a.persistWorkBotSettingsLocked(binding, expected, next, evidence.PrincipalID)
	if err != nil {
		return applied, err
	}
	applied.Evidence = "Bot settings persisted; active native executions retain their frozen configuration."
	if err = a.syncWorkRolesLocked(ctx, a.teamRuntime.store, binding.RoleID); err != nil {
		return applied, err
	}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		fresh, err := tx.GetRoleBotBinding(binding.RoleID)
		if err != nil {
			return err
		}
		if fresh != binding {
			return fmt.Errorf("settings persisted under the original Bot; current identity changed before role projection")
		}
		role, err := tx.GetRole(binding.RoleID)
		if err != nil {
			return err
		}
		if err := tx.CheckRoleSettingsReference(role.ID, applied.Fingerprint, role.SettingsRevision); err != nil {
			return err
		}
		applied.DesiredRevision = role.SettingsRevision
		return nil
	})
	return applied, err
}
func (a *App) persistWorkBotSettingsLocked(binding work.RoleBotBinding, expected string, next state.BotCapabilitySettingsRecord, principal string) (string, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return "", err
	}
	configured := false
	for _, app := range a.runtimeGatewayApps(loaded.Config) {
		if app.GatewayID == binding.GatewayRef && app.AppID == binding.AppID && app.Enabled {
			configured = true
			break
		}
	}
	if !configured {
		return "", worksettings.ErrConflict
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.workBotSettingsLocked(binding.GatewayRef)
	if a.resolveWorkBotSettings(current, loaded.Config).fingerprint != expected {
		return "", worksettings.ErrConflict
	}
	if !a.botCapabilitySettingsState.writable() || a.botCapabilitySettingsState.store == nil {
		return "", worksettings.ErrUnavailable
	}
	next.UpdatedBy = principal
	next.UpdatedAt = time.Now().UTC()
	next, valid := state.NormalizeBotCapabilitySettingsRecord(next)
	if !valid {
		return "", worksettings.ErrUnavailable
	}
	resolved := a.resolveWorkBotSettings(next, loaded.Config)
	if resolved.err != nil {
		return "", fmt.Errorf("%w: selected profile is no longer available: %v", worksettings.ErrConflict, resolved.err)
	}
	if err := a.botCapabilitySettingsState.store.Put(next); err != nil {
		return "", err
	}
	a.materializeBotCapabilitySettingsStateLocked()
	return resolved.fingerprint, nil
}

func (a *App) teamPreferencesService() (*worksettings.Service, error) {
	if a.teamRuntime == nil {
		return nil, worksettings.ErrUnavailable
	}
	return worksettings.New(a.teamRuntime.store, teamBotSettingsPort{a}, func(_ context.Context, principal string, binding work.RoleBotBinding, source work.SourceRef) error {
		// Existing product policy: an authenticated person's private conversation may
		// manage that explicitly bound Bot. Source validation enforces private scope.
		if principal == "" || binding.Validate() != nil || source.Validate() != nil {
			return workstore.ErrScopeDenied
		}
		return nil
	}, nil)
}
