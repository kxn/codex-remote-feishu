package daemon

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/codexcatalog"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) teamCatalogConnection(ctx context.Context, binding work.RoleBotBinding, profileID string) (string, error) {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return "", err
	}
	native, reserved, envKeys, failed := a.effectiveCodexNativeConnectionLocked()
	resolver := codexprofile.RuntimeResolver{APIProfiles: loaded.Config.Codex.Profiles, Preference: func(ref state.CodexContextPreferenceRef) (state.ProfileContextPreference, bool) {
		return preferenceStore.CodexRevision(ref.ProfileID, ref.Revision)
	}, Native: native, ReservedProviderIDs: reserved, NativeProviderEnvKeys: envKeys, NativeConfigProbeFailed: failed, CapabilitySet: a.effectiveCodexRuntimeCapabilitySetLocked(), ManagedModelCatalogDir: codexcatalog.ManagedModelCatalogDir(a.headlessRuntime.Paths.StateDir)}
	revision := uint64(0)
	if profileID == config.CodexOAuthProfileID {
		oauth, ok := a.codexOAuthProfileState.current()
		if !ok {
			return "", &codexprofile.RuntimeError{Code: codexprofile.ErrorOAuthProbeUnknown}
		}
		resolver.OAuthState = &oauth
		revision = oauth.Revision
	} else if profileID != config.CodexNativeProfileID {
		index := config.IndexOfCodexAPIProfile(loaded.Config.Codex.Profiles, profileID)
		if index < 0 {
			return "", &codexprofile.RuntimeError{Code: codexprofile.ErrorProfileRevisionUnavailable}
		}
		profile, ok := config.CurrentCodexAPIProfile(loaded.Config.Codex.Profiles[index])
		if !ok {
			return "", &codexprofile.RuntimeError{Code: codexprofile.ErrorProfileRevisionUnavailable}
		}
		revision = profile.Revision
	}
	pref, ok := preferenceStore.CodexCurrent(profileID)
	if !ok {
		return "", &codexprofile.RuntimeError{Code: codexprofile.ErrorProfileRevisionUnavailable}
	}
	projection, err := resolver.Resolve(state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: profileID, Revision: revision}, ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: profileID, Revision: pref.Revision}})
	if err != nil {
		return "", err
	}
	return projection.Connection.ConnectionContractID, nil
}
