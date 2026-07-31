package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type codexNativeConnectionRuntimeState struct {
	checked                 bool
	checking                bool
	probeSucceeded          bool
	evidence                state.CodexNativeConnectionEvidence
	reservedProviderIDs     []string
	reservedProviderEnvKeys []string
	done                    chan struct{}
}

func (a *App) ensureCodexNativeConnectionEvidence(ctx context.Context) {
	a.mu.Lock()
	if a.codexNativeConnection.checked {
		a.mu.Unlock()
		return
	}
	if a.codexNativeConnection.checking {
		done := a.codexNativeConnection.done
		a.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-done:
		}
		return
	}
	a.codexNativeConnection.checking = true
	a.codexNativeConnection.done = make(chan struct{})
	done := a.codexNativeConnection.done
	runProbe := a.runCodexNativeConfigProbe
	options := codexprofile.NativeConfigProbeOptions{
		BinaryPath: a.headlessRuntime.CodexRealBinary,
		Env:        append([]string{}, a.headlessRuntime.BaseEnv...),
		Version:    a.serverIdentity.Version,
	}
	connectionGeneration := codexNativeConnectionGeneration(a.daemonLifecycleID)
	a.mu.Unlock()

	observation := codexprofile.NativeConfigObservation{}
	probeSucceeded := false
	if runProbe != nil && strings.TrimSpace(options.BinaryPath) != "" {
		probeCtx, cancel := context.WithTimeout(ctx, codexOAuthProbeTimeout)
		probed, err := runProbe(probeCtx, options)
		cancel()
		if err == nil {
			observation = probed
			probeSucceeded = true
		}
	}
	evidence := codexprofile.ProjectNativeConnectionEvidence(observation, connectionGeneration)

	a.mu.Lock()
	a.codexNativeConnection.checked = true
	a.codexNativeConnection.checking = false
	a.codexNativeConnection.probeSucceeded = probeSucceeded
	a.codexNativeConnection.evidence = evidence
	a.codexNativeConnection.reservedProviderIDs = append([]string{}, observation.ProviderIDs...)
	a.codexNativeConnection.reservedProviderEnvKeys = append([]string{}, observation.ProviderEnvKeys...)
	close(done)
	a.mu.Unlock()
}

func (a *App) effectiveCodexNativeConnectionLocked() (state.CodexNativeConnectionEvidence, []string, []string, bool) {
	if a.codexNativeConnection.checked {
		return a.codexNativeConnection.evidence,
			append([]string{}, a.codexNativeConnection.reservedProviderIDs...),
			append([]string{}, a.codexNativeConnection.reservedProviderEnvKeys...),
			!a.codexNativeConnection.probeSucceeded
	}
	evidence := codexprofile.ProjectNativeConnectionEvidence(
		codexprofile.NativeConfigObservation{},
		codexNativeConnectionGeneration(a.daemonLifecycleID),
	)
	return evidence, nil, nil, false
}

func codexNativeConnectionGeneration(lifecycleID string) uint64 {
	sum := sha256.Sum256([]byte("codex-native-connection-lifecycle-v1\x00" + strings.TrimSpace(lifecycleID)))
	generation := binary.BigEndian.Uint64(sum[:8])
	if generation == 0 {
		return 1
	}
	return generation
}
