package daemon

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/codexcatalog"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamProfileCatalogTarget struct {
	Key                string
	Options            codexprofile.ModelCatalogProbeOptions
	Launch             codexprofile.SecretLaunchMaterial
	NativeGuard        string
	NativeEnv          []string
	NativeProviderKeys []string
}

func (a *App) teamProfileCatalog(ctx context.Context, binding work.RoleBotBinding, profileID, cursor string) (teamProfileCatalogTarget, error) {
	return a.withTeamProfileCatalog(ctx, binding, profileID, cursor, nil)
}

func (a *App) withTeamProfileCatalog(ctx context.Context, binding work.RoleBotBinding, profileID, cursor string, consume func(teamProfileCatalogTarget) error) (teamProfileCatalogTarget, error) {
	return a.withProfileCatalog(ctx, profileCatalogScope{binding.RoleID, binding.GatewayRef, binding.AppID, binding.IdentityGeneration}, profileID, cursor, consume)
}

// profileCatalogScope identifies the configured bot, independent of a team
// role or an active execution carrier. It preserves the existing cache key.
type profileCatalogScope struct {
	RoleID, GatewayID, AppID string
	BotGeneration            int64
}

func (a *App) withProfileCatalog(ctx context.Context, scope profileCatalogScope, profileID, cursor string, consume func(teamProfileCatalogTarget) error) (teamProfileCatalogTarget, error) {
	var target teamProfileCatalogTarget
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return target, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return target, err
	}
	native, reserved, envKeys, failed := a.effectiveCodexNativeConnectionLocked()
	resolver := codexprofile.RuntimeResolver{APIProfiles: loaded.Config.Codex.Profiles, Preference: func(ref state.CodexContextPreferenceRef) (state.ProfileContextPreference, bool) {
		return preferenceStore.CodexRevision(ref.ProfileID, ref.Revision)
	}, Native: native, ReservedProviderIDs: reserved, NativeProviderEnvKeys: envKeys, NativeConfigProbeFailed: failed, CapabilitySet: a.effectiveCodexRuntimeCapabilitySetLocked(), ManagedModelCatalogDir: codexcatalog.ManagedModelCatalogDir(a.headlessRuntime.Paths.StateDir)}
	revision := uint64(0)
	if profileID == config.CodexOAuthProfileID {
		oauth, ok := a.codexOAuthProfileState.current()
		if !ok {
			return target, &codexprofile.RuntimeError{Code: codexprofile.ErrorOAuthProbeUnknown}
		}
		resolver.OAuthState = &oauth
		revision = oauth.Revision
	} else if profileID != config.CodexNativeProfileID {
		index := config.IndexOfCodexAPIProfile(loaded.Config.Codex.Profiles, profileID)
		if index < 0 {
			return target, &codexprofile.RuntimeError{Code: codexprofile.ErrorProfileRevisionUnavailable}
		}
		profile, ok := config.CurrentCodexAPIProfile(loaded.Config.Codex.Profiles[index])
		if !ok {
			return target, &codexprofile.RuntimeError{Code: codexprofile.ErrorProfileRevisionUnavailable}
		}
		revision = profile.Revision
	}
	pref, ok := preferenceStore.CodexCurrent(profileID)
	if !ok {
		return target, &codexprofile.RuntimeError{Code: codexprofile.ErrorProfileRevisionUnavailable}
	}
	projection, err := resolver.Resolve(state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: profileID, Revision: revision}, ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: profileID, Revision: pref.Revision}})
	if err != nil {
		return target, err
	}
	// Execution generations deliberately do not identify reusable directory data.
	connection := projection.Connection
	connection.ConnectionContractID = ""
	connection.ProfileRef.Revision = 0
	account := ""
	if profileID == config.CodexOAuthProfileID {
		oauth := resolver.OAuthState
		if oauth.AccountFingerprint == "" {
			return target, fmt.Errorf("尚未取得登录账号信息，请刷新重试")
		}
		connection.ConnectionGeneration = 0
		account = oauth.AccountFingerprint + ":" + oauth.PlanType
		target.Options.ExpectedAccountFingerprint = oauth.AccountFingerprint
		target.Options.ExpectedPlanType = oauth.PlanType
	} else if profileID == config.CodexNativeProfileID {
		connection.ConnectionGeneration = 0
		account, err = nativeCatalogCredentialIdentity(a.headlessRuntime.BaseEnv, envKeys)
		if err == nil {
			target.NativeGuard, err = nativeCatalogCredentialGuard(a.headlessRuntime.BaseEnv, envKeys)
		}
		target.NativeEnv = append([]string{}, a.headlessRuntime.BaseEnv...)
		target.NativeProviderKeys = append([]string{}, envKeys...)
		if err != nil {
			return target, err
		}
	}
	target.Key = workui.ID("profile-model-catalog-v1", scope.RoleID, scope.GatewayID, scope.AppID, fmt.Sprint(scope.BotGeneration), profileID, fmt.Sprintf("%+v", connection), account, cursor)
	target.Launch = projection.Launch
	target.Options.Env, target.Options.Args = codexprofile.ApplyLaunchMaterial(a.headlessRuntime.BaseEnv, []string{"app-server", "--listen=stdio://"}, projection.Launch)
	target.Options.BinaryPath = a.headlessRuntime.CodexRealBinary
	target.Options.Version = a.serverIdentity.Version
	target.Options.Cursor, target.Options.Limit = cursor, 50
	if consume != nil {
		err = consume(target)
	}
	return target, err
}
