package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/atomicfile"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type privateNativeLaunchRequest struct {
	Record                                                                privateCarrierRecord
	CodexProfileID, ClaudeProfileID, ClaudeReasoningEffort, ResumeSession string
	CodexAdmissionRef                                                     *state.CodexAdmissionRef
	OpenCodeProfileID, OpenCodeRuntimeAccessMode                          string
	OpenCodeAdmissionRef                                                  *state.OpenCodeAdmissionRef
	MigrationResume                                                       *privatechat.MigrationResumeIntent
	UpgradeRecovery                                                       *privatechat.UpgradeRecoveryPlan
	UpgradeRecoveryEffectID                                               string
	resumePrepared                                                        bool
}

// launchPrivateNative freezes the actual selected CWD, profile environment and
// native history namespace before starting. Caller supplies a trusted surface
// selection and fresh carrier identity; no team attempt or fallback CWD exists.
func (a *App) launchPrivateNative(ctx context.Context, h *privateNativeHost, request privateNativeLaunchRequest) (int, error) {
	rec := request.Record
	o := rec.Snapshot.Owner
	cfg := a.headlessRuntime
	if h == nil || h.store == nil || h.registry == nil || o.HostID != h.hostID || rec.SurfaceGeneration <= 0 {
		return 0, fmt.Errorf("invalid private carrier")
	}
	if request.UpgradeRecovery != nil {
		plan, ok, err := h.journal.UpgradeRecoverySelection(ctx, rec.Surface, rec.SurfaceGeneration, request.UpgradeRecoveryEffectID)
		if err != nil || !ok || plan != *request.UpgradeRecovery || plan.OldOwner.HistoryNamespace != o.HistoryNamespace || plan.OldOwner.Backend != o.Backend || plan.OldOwner.OwnerID != o.OwnerID || plan.OldOwner.Epoch != o.Epoch || o.InstanceGeneration <= plan.OldOwner.InstanceGeneration || request.ResumeSession != plan.ThreadID || rec.Snapshot.CWD != plan.CWD {
			return 0, executionowner.ErrConflict
		}
		effect, err := h.journal.LoadEffect(ctx, rec.Surface, rec.SurfaceGeneration, request.UpgradeRecoveryEffectID)
		if err != nil {
			return 0, err
		}
		event, _, err := privatechat.DecodeStoredEffect(effect.Effect.Body)
		if err != nil || event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandStartHeadless || event.DaemonCommand.InstanceID != o.InstanceID || event.DaemonCommand.Backend != o.Backend || event.DaemonCommand.ThreadID != plan.ThreadID {
			return 0, executionowner.ErrConflict
		}
	}
	if o.Application != "private" || (o.Backend != agentproto.BackendCodex && o.Backend != agentproto.BackendClaude && o.Backend != agentproto.BackendOpenCode) || (request.resumePrepared && o.HistoryNamespace == "") {
		return 0, fmt.Errorf("invalid private launch selection")
	}
	if request.MigrationResume != nil && request.MigrationResume.EvidenceVersion == "legacy-codex-v1" && (o.Backend != agentproto.BackendCodex || request.MigrationResume.LegacyEvidenceID == "" || request.MigrationResume.LegacyEvidenceHash == "") {
		return 0, fmt.Errorf("invalid legacy codex migration evidence")
	}
	if err := validateHeadlessWorkDir(rec.Snapshot.CWD); err != nil {
		return 0, err
	}
	if !filepath.IsAbs(cfg.Paths.StateDir) || strings.TrimSpace(cfg.BinaryPath) == "" {
		return 0, fmt.Errorf("native launcher unavailable")
	}
	if _, err := runtimejournal.ValidatePath(rec.Snapshot.JournalPath, cfg.Paths.StateDir, rec.Snapshot.CWD); err != nil {
		return 0, err
	}
	if h.startHeadless == nil {
		return 0, fmt.Errorf("native launcher unavailable")
	}
	if o.Backend == agentproto.BackendCodex && a.profileCatalogMigrationErr != nil {
		return 0, a.profileCatalogMigrationErr
	}
	env := make([]string, 0, len(cfg.BaseEnv))
	for _, entry := range cfg.BaseEnv {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case config.WorkLaunchSnapshotEnv, config.WorkSessionTokenEnv, config.WorkToolEndpointEnv, config.WorkSessionCredentialRefEnv, config.WorkOwnerBindingEnv, config.WorkTeamBinaryEnv, agentproto.PrivateNativeLaunchEnv, agentproto.PrivateNativeCredentialEnv, agentproto.TeamRelayCredentialEnv:
			continue
		}
		env = append(env, entry)
	}
	args := append([]string{}, cfg.LaunchArgs...)
	a.mu.Lock()
	env, args, _, err := a.applyCodexHeadlessProfileConfigLocked(env, args, o.Backend, request.CodexProfileID, request.CodexAdmissionRef)
	a.mu.Unlock()
	if err != nil {
		return 0, err
	}
	a.mu.Lock()
	env, args, _, err = a.applyOpenCodeHeadlessProfileConfigLocked(env, args, o.Backend, control.DaemonCommand{InstanceID: o.InstanceID, WorkspaceKey: rec.Snapshot.CWD, ThreadCWD: rec.Snapshot.CWD, OpenCodeProfileID: request.OpenCodeProfileID, OpenCodeAdmissionRef: request.OpenCodeAdmissionRef, OpenCodeRuntimeAccessMode: request.OpenCodeRuntimeAccessMode})
	a.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if o.Backend == agentproto.BackendOpenCode {
		if err = a.probePrivateOpenCodeVersion(ctx, env, rec.Snapshot.CWD); err != nil {
			return 0, err
		}
	}
	a.mu.Lock()
	var claude config.ClaudeRuntimeSettings
	if request.MigrationResume != nil && request.MigrationResume.EvidenceVersion != "legacy-codex-v1" {
		// Source connection identity is defined by the team Claude resolver.
		// Resolve and apply the very same profile and environment snapshot.
		if o.Backend != agentproto.BackendClaude || request.MigrationResume.ConnectionIdentity == "" {
			a.mu.Unlock()
			return 0, fmt.Errorf("migration connection identity unavailable")
		}
		loaded, e := a.loadAdminConfig()
		if e != nil {
			a.mu.Unlock()
			return 0, e
		}
		profile, _, e := a.resolveClaudeLaunchProfile(loaded.Config, request.ClaudeProfileID, state.ModelConfigRecord{})
		if e != nil {
			a.mu.Unlock()
			return 0, e
		}
		identity, e := teamClaudeConnectionIdentity(env, profile, rec.Snapshot.CWD)
		if e != nil || identity != request.MigrationResume.ConnectionIdentity {
			a.mu.Unlock()
			return 0, fmt.Errorf("migration connection identity changed")
		}
		env, claude, err = applyResolvedClaudeLaunchProfile(env, profile)
	} else {
		env, claude, err = a.applyClaudeHeadlessProfileEnv(env, o.Backend, request.ClaudeProfileID)
	}
	a.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if o.Backend == agentproto.BackendClaude {
		if effort := state.NormalizeClaudeReasoningEffort(request.ClaudeReasoningEffort); effort != "" {
			env = config.ApplyClaudeReasoningLaunchEnv(env, effort)
			claude = config.MergeClaudeRuntimeSettings(claude, config.ClaudeReasoningRuntimeSettings(effort))
		}
		if !claude.Empty() {
			raw, e := config.MarshalClaudeRuntimeSettings(claude)
			if e != nil {
				return 0, e
			}
			env = config.UpsertEnvValue(env, config.ClaudeRuntimeSettingsJSONEnv, raw)
		}
	}
	ns, err := nativeHistoryNamespace(o.Backend, env, rec.Snapshot.CWD, o.HostID)
	if err != nil {
		return 0, err
	}
	if o.HistoryNamespace != "" && ns != o.HistoryNamespace {
		return 0, fmt.Errorf("native history namespace changed")
	}
	if request.ResumeSession != "" && request.MigrationResume == nil && request.UpgradeRecovery == nil {
		if h.journal == nil {
			return 0, fmt.Errorf("private history authority unavailable")
		}
		internal, e := h.journal.IsInternalNativeSession(ctx, string(o.Backend), ns, request.ResumeSession)
		if e != nil {
			return 0, e
		}
		if internal {
			return 0, fmt.Errorf("internal native session cannot be resumed")
		}
		known, allowed, e := h.registry.SessionAccess(ctx, string(o.Backend), ns, request.ResumeSession, privateRegistration(o).Owner)
		if e != nil {
			return 0, e
		}
		if known && !allowed {
			return 0, fmt.Errorf("native session belongs to another application owner")
		}
		if !known {
			restricted, e := teamHistoricalSession(ctx, h.teamHistory, o.Backend, ns, request.ResumeSession)
			if e != nil {
				return 0, e
			}
			if restricted {
				return 0, fmt.Errorf("native session has unresolved team ownership")
			}
		}
	}
	// The namespace is a result of the actual selected profile environment,
	// not a field a fresh caller must independently predict. Nothing is frozen
	// or launched until the complete owner and snapshot validate here.
	o.HistoryNamespace = ns
	rec.Snapshot.Owner = o
	if err := rec.Snapshot.Validate(); err != nil {
		return 0, err
	}
	rec.OpenCodeProfileID = request.OpenCodeProfileID
	rec.OpenCodeAdmissionRef = state.NormalizeOpenCodeAdmissionRef(request.OpenCodeAdmissionRef)
	rec.OpenCodeRuntimeAccessMode = request.OpenCodeRuntimeAccessMode
	rec.CodexProfileID = request.CodexProfileID
	rec.ClaudeProfileID = request.ClaudeProfileID
	rec.ClaudeReasoningEffort = request.ClaudeReasoningEffort
	rec.ResumeSession = request.ResumeSession
	rec.CodexAdmissionRef = state.NormalizeCodexAdmissionRef(request.CodexAdmissionRef)
	rec.MigrationResume = request.MigrationResume
	rec.UpgradeRecovery = request.UpgradeRecovery
	rec.UpgradeRecoveryEffectID = request.UpgradeRecoveryEffectID
	fingerprintBytes, err := json.Marshal(struct {
		Snapshot                                    agentproto.PrivateNativeLaunchSnapshot
		Binary, Config                              string
		Env, Args                                   []string
		CodexProfile, ClaudeProfile, Effort, Resume string
		Admission                                   *state.CodexAdmissionRef
		OpenCodeProfile                             string                      `json:"OpenCodeProfile,omitempty"`
		OpenCodeAccess                              string                      `json:"OpenCodeAccess,omitempty"`
		OpenCodeAdmission                           *state.OpenCodeAdmissionRef `json:"OpenCodeAdmission,omitempty"`
	}{rec.Snapshot, cfg.BinaryPath, cfg.ConfigPath, env, args, rec.CodexProfileID, rec.ClaudeProfileID, rec.ClaudeReasoningEffort, rec.ResumeSession, rec.CodexAdmissionRef, rec.OpenCodeProfileID, rec.OpenCodeRuntimeAccessMode, rec.OpenCodeAdmissionRef})
	if err != nil {
		return 0, err
	}
	fingerprint := sha256.Sum256(fingerprintBytes)
	rec.ConfigurationFingerprint = hex.EncodeToString(fingerprint[:])
	var secret string
	a.profileReferenceGate.RLock()
	err = func() error {
		if request.resumePrepared {
			stored, e := h.store.lookup(ctx, o.InstanceID)
			if e != nil {
				return e
			}
			if stored.State != "prepared" || stored.Snapshot != rec.Snapshot || stored.Surface != rec.Surface || stored.SurfaceGeneration != rec.SurfaceGeneration || stored.ConfigurationFingerprint != rec.ConfigurationFingerprint || !reflect.DeepEqual(stored.MigrationResume, rec.MigrationResume) || !reflect.DeepEqual(stored.UpgradeRecovery, rec.UpgradeRecovery) || stored.UpgradeRecoveryEffectID != rec.UpgradeRecoveryEffectID {
				return fmt.Errorf("frozen private launch configuration changed")
			}
			secret, e = h.store.credential(ctx, stored)
			if e != nil {
				return e
			}
			rec = stored
		} else {
			var e error
			secret, e = h.store.prepare(ctx, rec)
			if e != nil {
				return e
			}
		}
		return nil
	}()
	a.profileReferenceGate.RUnlock()
	if err != nil {
		return 0, err
	}
	if request.UpgradeRecovery != nil {
		m, e := h.registry.MaintenanceStatus(ctx, string(o.Backend))
		if e != nil || m.ID != request.UpgradeRecovery.MaintenanceID || m.Epoch != request.UpgradeRecovery.MaintenanceEpoch || m.Phase != "restoring" {
			return 0, executionowner.ErrConflict
		}
		old, next := privateRegistration(request.UpgradeRecovery.OldOwner), privateRegistration(o)
		if err = h.registry.RegisterMaintenanceRecovery(ctx, m, old, next); err != nil {
			return 0, err
		}
		if request.ResumeSession != "" {
			if err = h.registry.ReserveMaintenanceSession(ctx, m, old, next, request.ResumeSession); err != nil {
				return 0, err
			}
		}
	} else {
		if err = h.registry.Register(ctx, privateRegistration(o)); err != nil {
			return 0, err
		}
		if request.ResumeSession != "" {
			if err = h.registry.ReserveSession(ctx, privateRegistration(o), request.ResumeSession); err != nil {
				return 0, err
			}
		}
	}
	data, err := json.Marshal(rec.Snapshot)
	if err != nil {
		return 0, err
	}
	path := filepath.Join(cfg.Paths.StateDir, "private-carriers", rec.Snapshot.Owner.CarrierIncarnation, "launch.json")
	// Incarnation is opaque, never a filesystem path component from untrusted input.
	if filepath.Base(o.CarrierIncarnation) != o.CarrierIncarnation || o.CarrierIncarnation == "." || o.CarrierIncarnation == ".." {
		return 0, fmt.Errorf("invalid incarnation path")
	}
	if err = atomicfile.Write(path, data, 0600); err != nil {
		return 0, err
	}
	for key, value := range map[string]string{agentproto.PrivateNativeLaunchEnv: path, agentproto.PrivateNativeCredentialEnv: secret, "CODEX_REMOTE_INSTANCE_ID": o.InstanceID, "CODEX_REMOTE_INSTANCE_SOURCE": "headless", "CODEX_REMOTE_INSTANCE_MANAGED": "1", "CODEX_REMOTE_LIFETIME": "daemon-owned", config.CodexRemoteInstanceBackendEnv: string(o.Backend)} {
		env = config.UpsertEnvValue(env, key, value)
	}
	if request.ResumeSession != "" {
		env = config.UpsertEnvValue(env, config.ResumeThreadIDEnv, request.ResumeSession)
	}
	if o.Backend == agentproto.BackendOpenCode {
		env = config.UpsertEnvValue(env, config.OpenCodeRuntimeProfileIDEnv, state.NormalizeOpenCodeProfileID(request.OpenCodeProfileID))
		if mode := state.NormalizeOpenCodeRuntimeAccessMode(request.OpenCodeRuntimeAccessMode); mode != "" {
			env = config.UpsertEnvValue(env, config.OpenCodeRuntimeAccessModeEnv, mode)
		}
	} else if o.Backend == agentproto.BackendCodex {
		env = config.UpsertEnvValue(env, config.CodexRuntimeProfileIDEnv, state.NormalizeCodexProfileID(request.CodexProfileID))
	} else {
		env = config.UpsertEnvValue(env, config.ClaudeRuntimeProfileIDEnv, state.NormalizeClaudeProfileID(request.ClaudeProfileID))
	}
	if err = h.store.transition(ctx, o.InstanceID, "prepared", "sending", hostinventory.ProcessIdentity{}); err != nil {
		return 0, err
	}
	pid, err := h.startHeadless(relayruntime.HeadlessLaunchOptions{BinaryPath: cfg.BinaryPath, ConfigPath: cfg.ConfigPath, Env: env, Paths: cfg.Paths, WorkDir: rec.Snapshot.CWD, InstanceID: o.InstanceID, LaunchMode: headlessLaunchModeForBackend(o.Backend), Args: args})
	if err != nil {
		return 0, err
	}
	process, err := hostinventory.CaptureProcess(pid)
	if err != nil {
		return 0, err
	}
	if err = h.store.transition(ctx, o.InstanceID, "sending", "running", process); err != nil {
		return 0, err
	}
	return pid, nil
}

// resumePreparedPrivateLaunch may only re-run a durable prepared intent: the
// sending transition always precedes process creation, so it proves no spawn.
func (a *App) resumePreparedPrivateLaunch(ctx context.Context, h *privateNativeHost, instance string) (int, error) {
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return 0, err
	}
	if rec.State != "prepared" {
		return 0, fmt.Errorf("launch not safely retryable")
	}
	return a.launchPrivateNative(ctx, h, privateNativeLaunchRequest{Record: rec, OpenCodeProfileID: rec.OpenCodeProfileID, OpenCodeAdmissionRef: rec.OpenCodeAdmissionRef, OpenCodeRuntimeAccessMode: rec.OpenCodeRuntimeAccessMode, CodexProfileID: rec.CodexProfileID, ClaudeProfileID: rec.ClaudeProfileID, ClaudeReasoningEffort: rec.ClaudeReasoningEffort, ResumeSession: rec.ResumeSession, CodexAdmissionRef: rec.CodexAdmissionRef, MigrationResume: rec.MigrationResume, UpgradeRecovery: rec.UpgradeRecovery, UpgradeRecoveryEffectID: rec.UpgradeRecoveryEffectID, resumePrepared: true})
}
