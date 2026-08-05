package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/codexoauthstate"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const codexOAuthProbeTimeout = 10 * time.Second

type codexRuntimeCapabilityState struct {
	checked       bool
	checking      bool
	capabilitySet string
	errorCode     string
	done          chan struct{}
}

type codexOAuthProfileRuntimeState struct {
	store          *codexoauthstate.Store
	err            error
	probeInFlight  bool
	probeCompleted bool
	probeDone      chan struct{}
}

func (s codexOAuthProfileRuntimeState) current() (state.CodexOAuthProfileState, bool) {
	if s.err != nil || s.store == nil || !s.probeCompleted {
		return state.CodexOAuthProfileState{}, false
	}
	return s.store.Current()
}

func (a *App) configureCodexOAuthProfileStateLocked(stateDir string) {
	store, err := codexoauthstate.LoadStore(codexoauthstate.StatePath(stateDir))
	a.codexOAuthProfileState = codexOAuthProfileRuntimeState{store: store, err: err}
}

func (a *App) requestCodexOAuthProbe(ctx context.Context, forceAuthGeneration bool) bool {
	a.mu.Lock()
	task, ok := a.beginCodexOAuthProbeLocked(forceAuthGeneration)
	a.mu.Unlock()
	if !ok {
		return false
	}

	go func() {
		probeCtx, cancel := context.WithTimeout(ctx, codexOAuthProbeTimeout)
		defer cancel()
		_ = a.runCodexOAuthProbeTask(probeCtx, task)
	}()
	return true
}

func (a *App) ensureCodexOAuthProfileForLaunchLocked(ctx context.Context) error {
	for {
		if a.codexOAuthProfileState.probeInFlight {
			done := a.codexOAuthProfileState.probeDone
			a.mu.Unlock()
			if done == nil {
				a.mu.Lock()
				return &codexprofile.RuntimeError{Code: codexprofile.ErrorOAuthProbeUnknown}
			}
			select {
			case <-ctx.Done():
				a.mu.Lock()
				return &codexprofile.RuntimeError{Code: codexprofile.ErrorOAuthProbeUnknown}
			case <-done:
				a.mu.Lock()
				return nil
			}
		}
		task, ok := a.beginCodexOAuthProbeLocked(false)
		if !ok {
			return &codexprofile.RuntimeError{Code: codexprofile.ErrorOAuthProbeUnknown}
		}
		a.mu.Unlock()
		probeCtx, cancel := context.WithTimeout(ctx, codexOAuthProbeTimeout)
		err := a.runCodexOAuthProbeTask(probeCtx, task)
		cancel()
		a.mu.Lock()
		return err
	}
}

type codexOAuthProbeTask struct {
	runProbe            func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error)
	options             codexprofile.OAuthProbeOptions
	lifecycleID         string
	capabilitySet       string
	forceAuthGeneration bool
	done                chan struct{}
}

func (a *App) beginCodexOAuthProbeLocked(forceAuthGeneration bool) (codexOAuthProbeTask, bool) {
	if a.codexOAuthProfileState.probeInFlight || a.codexOAuthProfileState.store == nil || a.codexOAuthProfileState.err != nil ||
		a.runCodexOAuthProbe == nil || strings.TrimSpace(a.headlessRuntime.CodexRealBinary) == "" {
		return codexOAuthProbeTask{}, false
	}
	done := make(chan struct{})
	a.codexOAuthProfileState.probeInFlight = true
	a.codexOAuthProfileState.probeDone = done
	return codexOAuthProbeTask{
		runProbe: a.runCodexOAuthProbe,
		options: codexprofile.OAuthProbeOptions{
			BinaryPath: a.headlessRuntime.CodexRealBinary,
			Env:        append([]string{}, a.headlessRuntime.BaseEnv...),
			Version:    a.serverIdentity.Version,
		},
		lifecycleID:         a.daemonLifecycleID,
		capabilitySet:       a.effectiveCodexRuntimeCapabilitySetLocked(),
		forceAuthGeneration: forceAuthGeneration,
		done:                done,
	}, true
}

func (a *App) runCodexOAuthProbeTask(ctx context.Context, task codexOAuthProbeTask) error {
	observation, err := task.runProbe(ctx, task.options)
	if err != nil {
		code := codexprofile.OAuthProbeErrorCode(err)
		if code == "" {
			code = codexprofile.ErrorOAuthProbeUnknown
		}
		observation = codexprofile.OAuthProbeObservation{
			Result: codexprofile.OAuthProbeResult{
				Status:             codexprofile.OAuthProbeStatusUnknown,
				LastProbeErrorCode: code,
			},
		}
	}
	observation.CapabilitySet = task.capabilitySet
	loaded, configErr := a.loadAdminConfig()

	a.mu.Lock()
	defer a.mu.Unlock()
	defer func() {
		a.codexOAuthProfileState.probeInFlight = false
		if a.codexOAuthProfileState.probeDone == task.done {
			a.codexOAuthProfileState.probeDone = nil
		}
		close(task.done)
	}()
	_, _, applyErr := a.codexOAuthProfileState.store.ApplyProbe(
		observation,
		time.Now().UTC(),
		task.lifecycleID,
		task.forceAuthGeneration,
	)
	if applyErr != nil {
		a.codexOAuthProfileState.err = applyErr
		return applyErr
	}
	preferenceStore, preferenceErr := a.profileContextPreferenceStore()
	if preferenceErr != nil {
		a.codexOAuthProfileState.err = preferenceErr
		return preferenceErr
	}
	if preferenceErr := preferenceStore.EnsureCodexProfile(state.OAuthCodexProfileID, state.CodexContextModeDefault); preferenceErr != nil {
		a.codexOAuthProfileState.err = preferenceErr
		return preferenceErr
	}
	a.codexOAuthProfileState.probeCompleted = true
	if configErr == nil {
		a.syncCodexProvidersCatalogLocked(loaded.Config)
	}
	return nil
}

func (a *App) ensureCodexRuntimeCapability(ctx context.Context) {
	a.mu.Lock()
	if a.codexRuntimeCapability.checked {
		a.mu.Unlock()
		return
	}
	if a.codexRuntimeCapability.checking {
		done := a.codexRuntimeCapability.done
		a.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-done:
		}
		return
	}
	a.codexRuntimeCapability.checking = true
	a.codexRuntimeCapability.done = make(chan struct{})
	done := a.codexRuntimeCapability.done
	runPreflight := a.runCodexCapabilityPreflight
	options := codexprofile.CapabilityPreflightOptions{
		BinaryPath: a.headlessRuntime.CodexRealBinary,
		Env:        append([]string{}, a.headlessRuntime.BaseEnv...),
		Version:    a.serverIdentity.Version,
	}
	a.mu.Unlock()

	capabilitySet := ""
	errorCode := ""
	if runPreflight == nil || strings.TrimSpace(options.BinaryPath) == "" {
		errorCode = codexprofile.ErrorCodexCapabilityUnsupported
	} else {
		preflightCtx, cancel := context.WithTimeout(ctx, codexOAuthProbeTimeout)
		observation, err := runPreflight(preflightCtx, options)
		cancel()
		if err != nil || strings.TrimSpace(observation.CapabilitySet) != codexprofile.CodexProfileCapabilitySetV1 {
			errorCode = codexprofile.ErrorCodexCapabilityUnsupported
		} else {
			capabilitySet = codexprofile.CodexProfileCapabilitySetV1
		}
	}

	a.mu.Lock()
	a.codexRuntimeCapability.checked = true
	a.codexRuntimeCapability.checking = false
	a.codexRuntimeCapability.capabilitySet = capabilitySet
	a.codexRuntimeCapability.errorCode = errorCode
	close(done)
	a.mu.Unlock()
}

func (a *App) effectiveCodexRuntimeCapabilitySetLocked() string {
	if a.codexRuntimeCapability.checked {
		return strings.TrimSpace(a.codexRuntimeCapability.capabilitySet)
	}
	// Production calls ensureCodexRuntimeCapability before ingress starts. The
	// pre-run value keeps direct component tests deterministic.
	return codexprofile.CodexProfileCapabilitySetV1
}
