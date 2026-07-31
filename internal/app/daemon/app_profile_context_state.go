package daemon

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/profilecontextstate"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type profileContextPreferenceRuntimeState = persistedStoreRuntimeState[*profilecontextstate.Store]

func (a *App) configureProfileContextPreferenceStateLocked(stateDir string) {
	path := profilecontextstate.StatePath(stateDir)
	runtimeState := loadPersistedStore("profile context preferences", path, profilecontextstate.LoadStore)
	if runtimeState.writable() {
		if err := runtimeState.store.EnsureCodexProfile(config.CodexNativeProfileID, state.CodexContextModeDefault); err != nil {
			runtimeState = degradedPersistedStore("profile context preferences", path, runtimeState.store, fmt.Errorf("ensure native profile: %w", err))
		}
		if runtimeState.writable() {
			if err := runtimeState.store.EnsureClaudeProfile(config.ClaudeDefaultProfileID, state.ClaudeContextModeDefault); err != nil {
				runtimeState = degradedPersistedStore("profile context preferences", path, runtimeState.store, fmt.Errorf("ensure claude default profile: %w", err))
			}
		}
	}
	a.profileContextPreferenceState = runtimeState
}

func (a *App) profileContextPreferenceStore() (*profilecontextstate.Store, error) {
	runtimeState := a.profileContextPreferenceState
	if !runtimeState.writable() || runtimeState.store == nil {
		if runtimeState.diagnosticErr != nil {
			return nil, fmt.Errorf("profile context preference store is degraded: %w", runtimeState.diagnosticErr)
		}
		return nil, fmt.Errorf("profile context preference store is unavailable")
	}
	return runtimeState.store, nil
}
