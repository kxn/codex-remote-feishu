package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamSettingsResolver struct {
	app    *App
	store  *workstore.Store
	hostID string
}

func teamID(prefix, value string) string {
	hash := sha256.Sum256([]byte(value))
	return prefix + "_" + hex.EncodeToString(hash[:20])
}

func (a *App) workBotSettingsLocked(gateway string) state.BotCapabilitySettingsRecord {
	if store := a.botCapabilitySettingsState.store; store != nil {
		if record, ok := store.Get(state.BotCapabilitySettingsKey(gateway)); ok {
			return record
		}
	}
	// Preserve the existing product's normalized native/default selection for
	// a newly configured Bot, rather than choosing an arbitrary model profile.
	record, _ := state.NormalizeBotCapabilitySettingsRecord(state.BotCapabilitySettingsRecord{GatewayID: gateway})
	return record
}

// The digest detects global profile changes without duplicating editable
// credentials in the work database. Only this digest crosses the store boundary.
type resolvedWorkBotSettings struct {
	fingerprint string
	claude      config.ClaudeProfile
	openCode    config.OpenCodeProfile
	err         error
}

func (a *App) resolveWorkBotSettings(record state.BotCapabilitySettingsRecord, cfg config.AppConfig) resolvedWorkBotSettings {
	var result resolvedWorkBotSettings
	var profile any
	switch record.Backend {
	case agentproto.BackendClaude:
		var preference state.ProfileContextPreference
		result.claude, preference, result.err = a.resolveClaudeLaunchProfile(cfg, record.ClaudeProfileID, record.PromptOverride)
		profile = struct {
			Profile            config.ClaudeProfile
			PreferenceRevision uint64
		}{result.claude, preference.Revision}
	case agentproto.BackendOpenCode:
		result.openCode, result.err = resolveOpenCodeLaunchProfile(cfg, record.OpenCodeProfileID, nil)
		profile = result.openCode
	default:
		var preference state.ProfileContextPreference
		if store, err := a.profileContextPreferenceStore(); err == nil {
			preference, _ = store.CodexCurrent(teamSettingsProfile(record))
		}
		profile = struct {
			Catalog            config.CodexSettings
			PreferenceRevision uint64
		}{cfg.Codex, preference.Revision}
	}
	// Those legacy values belong to surface execution, not team work admission.
	record.PromptOverride.AccessMode = ""
	record.PlanMode = ""
	record.PlanModeOverrideSet = false
	record.OpenCodeAdmissionRef = nil
	raw, _ := json.Marshal(struct {
		Bot     state.BotCapabilitySettingsRecord
		Profile any
	}{record, profile})
	hash := sha256.Sum256(raw)
	result.fingerprint = hex.EncodeToString(hash[:])
	return result
}

func (a *App) syncWorkRoles(ctx context.Context, store *workstore.Store) error {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	return a.syncWorkRolesLocked(ctx, store, "")
}

func (a *App) workSettingsAdmission(store *workstore.Store) workengine.SettingsAdmission {
	return func(ctx context.Context, roleID string, commit func() error) error {
		// Identity replacement can also clear the Bot's settings.
		a.feishuApplyMu.Lock()
		defer a.feishuApplyMu.Unlock()
		a.adminConfigMu.Lock()
		defer a.adminConfigMu.Unlock()
		if err := a.syncWorkRolesLocked(ctx, store, roleID); err != nil {
			return err
		}
		return commit()
	}
}

func (a *App) syncWorkRolesLocked(ctx context.Context, store *workstore.Store, onlyRoleID string) error {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return err
	}
	apps := a.runtimeGatewayApps(loaded.Config)
	for _, app := range apps {
		id := teamID("role", app.GatewayID)
		if !app.Enabled || app.AppID == "" || onlyRoleID != "" && id != onlyRoleID {
			continue
		}
		a.mu.Lock()
		settings := a.workBotSettingsLocked(app.GatewayID)
		identityGeneration := int64(1)
		if a.feishuBotIdentityState.store != nil {
			if identity, ok := a.feishuBotIdentityState.store.Get(app.GatewayID); ok && identity.AppID == app.AppID && identity.Pending == nil && identity.Generation > 0 {
				identityGeneration = int64(identity.Generation)
			}
		}
		a.mu.Unlock()
		fingerprint := a.resolveWorkBotSettings(settings, loaded.Config).fingerprint
		err = store.Within(ctx, func(tx *workstore.Tx) error {
			role, e := tx.GetRole(id)
			if errors.Is(e, workstore.ErrNotFound) {
				name := strings.TrimSpace(app.Name)
				if name == "" {
					name = app.GatewayID
				}
				role = work.Role{ID: id, Name: name, Description: "与用户持续协作，按当前项目分工完成工作并保存必要记忆。职责可以在后续协作中明确调整。", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
				if e = tx.PutRole(role, 0); e != nil {
					return e
				}
			} else if e != nil {
				return e
			}
			binding, e := tx.GetRoleBotBinding(id)
			if errors.Is(e, workstore.ErrNotFound) {
				if e = tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: id, GatewayRef: app.GatewayID, AppID: app.AppID, IdentityGeneration: identityGeneration}, 0); e != nil {
					return e
				}
			} else if e != nil {
				return e
			} else if binding.AppID != app.AppID {
				previous := binding.IdentityGeneration
				binding.IdentityGeneration++
				binding.AppID = app.AppID
				if e = tx.PutRoleBotBinding(binding, previous); e != nil {
					return e
				}
			}
			return tx.SyncRoleSettingsReference(id, fingerprint)
		})
		if err != nil {
			return err
		}
		if onlyRoleID != "" {
			return nil
		}
	}
	if onlyRoleID != "" {
		return workstore.ErrConflict
	}
	return nil
}

func (r teamSettingsResolver) ResolveLaunch(ctx context.Context, scope work.WorkScope, role work.Role, wc work.WorkContext) (workhost.LaunchSettings, error) {
	var result workhost.LaunchSettings
	location, err := r.location(ctx, scope, wc)
	if err != nil {
		return result, err
	}
	var binding work.RoleBotBinding
	if err = r.store.Within(ctx, func(tx *workstore.Tx) error { var e error; binding, e = tx.GetRoleBotBinding(role.ID); return e }); err != nil {
		return result, err
	}
	a := r.app
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return result, err
	}
	configured := false
	for _, app := range a.runtimeGatewayApps(loaded.Config) {
		if app.Enabled && app.GatewayID == binding.GatewayRef && app.AppID == binding.AppID {
			configured = true
			break
		}
	}
	if !configured {
		return result, fmt.Errorf("the role's Bot identity is disabled or has changed")
	}
	a.mu.Lock()
	record := a.workBotSettingsLocked(binding.GatewayRef)
	resolved := a.resolveWorkBotSettings(record, loaded.Config)
	a.mu.Unlock()
	if resolved.err != nil {
		return result, resolved.err
	}
	result = workhost.LaunchSettings{Backend: work.Backend(record.Backend), Location: location, Environment: append([]string{}, a.headlessRuntime.BaseEnv...), LaunchArgs: append([]string{}, a.headlessRuntime.LaunchArgs...), Overrides: agentproto.PromptOverrides{Model: record.PromptOverride.Model, ReasoningEffort: record.PromptOverride.ReasoningEffort}}
	// Preserve the old effective private default explicitly. An empty native
	// override otherwise inherits the backend's potentially stricter policy.
	// Project and read-only review authority is configured independently.
	result.Overrides.AccessMode = teamPrivateDefaultAccess(scope, wc, record)
	result.Overrides.PlanMode = teamPrivateDefaultPlan(scope, wc, record)

	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.CheckRoleSettingsReference(role.ID, resolved.fingerprint, role.SettingsRevision); e != nil {
			return e
		}
		preferences, e := tx.GetContextPreferences(wc.ID)
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		result.PreferencesRevision = preferences.Revision
		if preferences.AccessMode != nil {
			result.Overrides.AccessMode = *preferences.AccessMode
		}
		if preferences.PlanMode != nil {
			result.Overrides.PlanMode = *preferences.PlanMode
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	result.ProfileID = teamSettingsProfile(record)
	switch record.Backend {
	case agentproto.BackendClaude:
		result.ConnectionIdentity, err = teamClaudeConnectionIdentity(result.Environment, resolved.claude, location.Cwd)
		if err != nil {
			return result, err
		}
		var settings config.ClaudeRuntimeSettings
		result.Environment, settings, err = applyResolvedClaudeLaunchProfile(result.Environment, resolved.claude)
		if err == nil && !settings.Empty() {
			var raw string
			raw, err = config.MarshalClaudeRuntimeSettings(settings)
			if err == nil {
				result.Environment = config.UpsertEnvValue(result.Environment, config.ClaudeRuntimeSettingsJSONEnv, raw)
			}
		}
	case agentproto.BackendOpenCode:
		result.Environment, result.LaunchArgs, _, err = a.compileOpenCodeHeadlessProfile(result.Environment, resolved.openCode, control.DaemonCommand{InstanceID: teamID("worker", wc.ID), WorkspaceKey: location.Cwd, OpenCodeRuntimeAccessMode: result.Overrides.AccessMode})
	default:
		a.mu.Lock()
		env, args, projection, e := a.applyCodexHeadlessProfileConfigLocked(result.Environment, result.LaunchArgs, agentproto.BackendCodex, result.ProfileID, nil)
		a.mu.Unlock()
		err = e
		result.Environment, result.LaunchArgs = env, args
		if e == nil && projection != nil {
			result.CodexResume = orchestrator.CodexResumePolicyFromContract(&projection.Connection, &projection.Thread)
		}
	}
	return result, err
}

func (r teamSettingsResolver) location(ctx context.Context, scope work.WorkScope, wc work.WorkContext) (work.ExecutionLocation, error) {
	var selected work.ExecutionLocation
	if wc.Purpose == work.PurposeControl {
		return r.scratch(wc)
	}
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		if wc.Purpose == work.PurposeReview {
			if wc.Review == nil {
				return workstore.ErrScopeDenied
			}
			var e error
			selected, e = tx.GetLocation(wc.Review.LocationID)
			if e != nil {
				return e
			}
			if selected.Generation != wc.Review.LocationGeneration {
				return workstore.ErrConflict
			}
			if wc.Review.ResourceScopeID != scope.ID {
				return tx.RequireProjectPrincipalAccess(wc.Review.ResourceScopeID, scope.PrincipalID, time.Now())
			}
			return nil
		}
		preference, e := tx.GetExecutionPreference(scope.ID)
		if errors.Is(e, workstore.ErrNotFound) {
			if _, desiredErr := tx.GetDesiredExecutionLocation(wc.ID); desiredErr == nil {
				return fmt.Errorf("prepared location has lost its selected resource authorization")
			} else if !errors.Is(desiredErr, workstore.ErrNotFound) {
				return desiredErr
			}
			return nil
		}
		if e != nil {
			return e
		}
		if preference.DefaultLocationID != "" {
			selected, e = tx.GetLocation(preference.DefaultLocationID)
		}
		if e != nil {
			return e
		}
		desired, desiredErr := tx.GetDesiredExecutionLocation(wc.ID)
		if desiredErr == nil {
			if !slices.Contains(preference.SelectedLocationIDs, desired.RootLocationID) {
				return fmt.Errorf("prepared location no longer belongs to selected code resources")
			}
			selected, e = tx.GetLocation(desired.LocationID)
			if e == nil && selected.Generation != desired.LocationGeneration {
				return fmt.Errorf("prepared location changed; its execution binding needs reconciliation")
			}
		} else if !errors.Is(desiredErr, workstore.ErrNotFound) {
			return desiredErr
		}
		return e
	})
	if err != nil {
		return selected, err
	}
	if selected.ID != "" {
		if selected.HostID != r.hostID {
			return selected, fmt.Errorf("selected execution host is not connected here")
		}
		return selected, nil
	}
	return r.scratch(wc)
}

func (r teamSettingsResolver) scratch(wc work.WorkContext) (work.ExecutionLocation, error) {
	var selected work.ExecutionLocation
	// No project code resource is selected by this path.
	// Non-code/bootstrap work has a durable isolated scratch location. This
	// does not select any user's repository and does not create Git worktrees.
	path := filepath.Join(r.app.headlessRuntime.Paths.StateDir, "workspaces", wc.ID)
	if err := os.MkdirAll(path, 0700); err != nil {
		return selected, err
	}
	return work.ExecutionLocation{ID: teamID("scratch", wc.ID), HostID: r.hostID, Cwd: path, Generation: 1, Revision: 1, IsolatedContextID: wc.ID}, nil
}

func teamPrivateDefaultAccess(scope work.WorkScope, wc work.WorkContext, record state.BotCapabilitySettingsRecord) string {
	if scope.Kind == work.ScopePersonal && wc.Purpose != work.PurposeReview {
		return agentproto.EffectiveAccessMode(record.PromptOverride.AccessMode)
	}
	return ""
}

func teamPrivateDefaultPlan(scope work.WorkScope, wc work.WorkContext, record state.BotCapabilitySettingsRecord) string {
	if scope.Kind == work.ScopePersonal && wc.Purpose != work.PurposeReview && record.PlanModeOverrideSet {
		return string(state.NormalizePlanModeSetting(record.PlanMode))
	}
	return ""
}
