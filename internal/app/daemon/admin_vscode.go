package daemon

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/editor"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/pathcompare"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

type vscodeDetectResponse struct {
	SSHSession                 bool                        `json:"sshSession"`
	RecommendedMode            string                      `json:"recommendedMode"`
	CurrentMode                string                      `json:"currentMode"`
	CurrentBinary              string                      `json:"currentBinary"`
	InstallStatePath           string                      `json:"installStatePath"`
	InstallState               *install.InstallState       `json:"installState,omitempty"`
	Settings                   editor.VSCodeSettingsStatus `json:"settings"`
	CandidateBundleEntrypoints []string                    `json:"candidateBundleEntrypoints,omitempty"`
	LatestBundleEntrypoint     string                      `json:"latestBundleEntrypoint,omitempty"`
	RecordedBundleEntrypoint   string                      `json:"recordedBundleEntrypoint,omitempty"`
	LatestShim                 editor.ManagedShimStatus    `json:"latestShim"`
	RecordedShim               *editor.ManagedShimStatus   `json:"recordedShim,omitempty"`
	NeedsShimReinstall         bool                        `json:"needsShimReinstall"`
}

type vscodeApplyRequest struct {
	Mode             string `json:"mode,omitempty"`
	SettingsPath     string `json:"settingsPath,omitempty"`
	BundleEntrypoint string `json:"bundleEntrypoint,omitempty"`
}

func (a *App) handleVSCodeDetect(w http.ResponseWriter, _ *http.Request) {
	payload, err := a.buildVSCodeDetectResponse()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "vscode_detect_failed",
			Message: "failed to detect vscode integration state",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) handleVSCodeApply(w http.ResponseWriter, r *http.Request) {
	var req vscodeApplyRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode vscode apply payload",
			Details: err.Error(),
		})
		return
	}

	if err := a.applyVSCodeIntegration(req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "vscode_apply_failed",
			Message: "failed to apply vscode integration",
			Details: err.Error(),
		})
		return
	}
	a.mu.Lock()
	a.invalidateVSCodeCompatibilityCacheLocked()
	a.mu.Unlock()
	payload, err := a.buildVSCodeDetectResponse()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "vscode_detect_failed",
			Message: "vscode integration applied, but detect failed afterwards",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) handleVSCodeDisable(w http.ResponseWriter, r *http.Request) {
	if err := a.uninstallVSCodeIntegration(); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "vscode_disable_failed",
			Message: "failed to disable vscode integration",
			Details: err.Error(),
		})
		return
	}
	a.mu.Lock()
	a.invalidateVSCodeCompatibilityCacheLocked()
	a.mu.Unlock()
	payload, err := a.buildVSCodeDetectResponse()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "vscode_detect_failed",
			Message: "vscode integration disabled, but detect failed afterwards",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) uninstallVSCodeIntegration() error {
	statePath := a.installStatePath()
	state, err := loadInstallStateIfPresent(statePath)
	if err != nil {
		return err
	}

	defaults, err := a.platformDefaults()
	if err != nil {
		return err
	}
	currentBinary, err := a.currentBinaryPath()
	if err != nil {
		return err
	}

	settingsPath := ""
	if state != nil {
		settingsPath = strings.TrimSpace(state.VSCodeSettingsPath)
	}
	if settingsPath == "" {
		settingsPath = defaults.VSCodeSettingsPath
	}

	// Collect every entrypoint that actually carries a managed shim installed
	// by this product. This must not depend on install-state.json existing:
	// when the state file was lost or cleared, the shim still lives on disk and
	// disable would otherwise report success without removing anything.
	recordedEntrypoint := ""
	if state != nil {
		recordedEntrypoint = strings.TrimSpace(state.BundleEntrypoint)
	}
	statuses, err := detectManagedShimStatuses(defaults.CandidateBundleEntrypoints, currentBinary)
	if err != nil {
		return err
	}
	targets, err := managedShimUninstallTargets(recordedEntrypoint, statuses, loadedConfigPath(a), statePath, currentBinary)
	if err != nil {
		return err
	}
	for _, entrypoint := range targets {
		if err := editor.UninstallManagedShim(entrypoint, settingsPath); err != nil {
			return err
		}
	}

	if state != nil {
		state.BundleEntrypoint = ""
		state.Integrations = nil
		if err := install.WriteState(statePath, *state); err != nil {
			return err
		}
	}
	return nil
}

// managedShimUninstallTargets returns every entrypoint that currently carries a
// managed shim installed by this product: the entrypoint recorded in install
// state (when present) plus every entrypoint whose sidecar points back at this
// install's config/state paths. Entrypoints that are not actually shimmed
// (status.Kind empty) are skipped, so unrelated bundle binaries are left alone.
func managedShimUninstallTargets(recordedEntrypoint string, statuses map[string]editor.ManagedShimStatus, configPath, statePath, currentBinary string) ([]string, error) {
	targets := []string{}
	seen := map[string]bool{}
	add := func(entrypoint string) {
		entrypoint = strings.TrimSpace(entrypoint)
		if entrypoint == "" || seen[entrypoint] {
			return
		}
		seen[entrypoint] = true
		targets = append(targets, entrypoint)
	}
	add(recordedEntrypoint)
	for _, entrypoint := range historicalManagedShimTargets(recordedEntrypoint, statuses, configPath, statePath) {
		add(entrypoint)
	}
	filtered := make([]string, 0, len(targets))
	for _, entrypoint := range targets {
		status, err := lookupManagedShimStatus(statuses, entrypoint, currentBinary)
		if err != nil {
			return nil, err
		}
		if status.Kind == "" {
			continue
		}
		filtered = append(filtered, entrypoint)
	}
	return filtered, nil
}

func (a *App) handleVSCodeReinstallShim(w http.ResponseWriter, r *http.Request) {
	var req vscodeApplyRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_request",
			Message: "failed to decode reinstall-shim payload",
			Details: err.Error(),
		})
		return
	}
	if err := a.reinstallVSCodeShim(req.BundleEntrypoint); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "vscode_reinstall_failed",
			Message: "failed to reinstall vscode managed shim",
			Details: err.Error(),
		})
		return
	}
	a.mu.Lock()
	a.invalidateVSCodeCompatibilityCacheLocked()
	a.mu.Unlock()
	payload, err := a.buildVSCodeDetectResponse()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "vscode_detect_failed",
			Message: "shim reinstalled, but detect failed afterwards",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) buildVSCodeDetectResponse() (vscodeDetectResponse, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return vscodeDetectResponse{}, err
	}
	admin := a.snapshotAdminRuntime()
	defaults, err := a.platformDefaults()
	if err != nil {
		return vscodeDetectResponse{}, err
	}
	currentBinary, err := a.currentBinaryPath()
	if err != nil {
		return vscodeDetectResponse{}, err
	}
	candidateStatuses, err := detectManagedShimStatuses(defaults.CandidateBundleEntrypoints, currentBinary)
	if err != nil {
		return vscodeDetectResponse{}, err
	}
	installStatePath := a.installStatePath()
	installState, err := loadInstallStateIfPresent(installStatePath)
	if err != nil {
		return vscodeDetectResponse{}, err
	}
	if installState != nil {
		normalized := *installState
		install.ApplyStateMetadata(&normalized, install.StateMetadataOptions{
			StatePath:       installStatePath,
			InstalledBinary: currentBinary,
			CurrentVersion:  a.currentBinaryVersion(),
		})
		installState = &normalized
	}

	settingsPath := defaults.VSCodeSettingsPath
	if installState != nil && strings.TrimSpace(installState.VSCodeSettingsPath) != "" {
		settingsPath = installState.VSCodeSettingsPath
	}
	settingsStatus, err := editor.DetectVSCodeSettings(settingsPath, currentBinary)
	if err != nil {
		return vscodeDetectResponse{}, err
	}

	latestEntrypoint := ""
	if len(defaults.CandidateBundleEntrypoints) > 0 {
		latestEntrypoint = defaults.CandidateBundleEntrypoints[0]
	}
	latestShim, err := lookupManagedShimStatus(candidateStatuses, latestEntrypoint, currentBinary)
	if err != nil {
		return vscodeDetectResponse{}, err
	}
	if latestShim.Kind == editor.ManagedShimKindTiny && !managedShimOwnedByInstall(latestShim, loaded.Path, installStatePath) {
		// The latest entrypoint carries a managed shim, but its sidecar does
		// not point back at this install (stale data dir, another instance, or
		// a foreign tool). Treat it as fully absent: detect must never report
		// the integration as enabled for a shim that disable cannot manage, and
		// must not nag about repairing a shim that is not ours. An entrypoint
		// with no sidecar at all keeps its raw status so an extension upgrade
		// that moved the entrypoint still triggers the reinstall/repair path.
		latestShim = editor.ManagedShimStatus{}
	}

	// The "recorded" entrypoint is derived from disk, not from install state:
	// the first candidate entrypoint that actually carries a managed shim from
	// this product. This keeps detect correct when install-state.json is
	// missing or stale (see the disable-empty-run bug fixed in d66a2430). The
	// same ownership rule as disable is applied so a shim that does not point
	// back at this install is never claimed as recorded.
	recordedEntrypoint := ""
	var recordedShim *editor.ManagedShimStatus
	for _, entrypoint := range defaults.CandidateBundleEntrypoints {
		status := candidateStatuses[entrypoint]
		if status.Kind != "" && status.Exists && managedShimOwnedByInstall(status, loaded.Path, installStatePath) {
			recordedEntrypoint = entrypoint
			recordedShim = &status
			break
		}
	}

	currentMode := strings.TrimSpace(loaded.Config.Wrapper.IntegrationMode)
	if currentMode == "" {
		currentMode = string(install.IntegrationManagedShim)
	}
	recommendedMode := string(install.IntegrationManagedShim)
	_ = admin
	needsReinstall := computeShimReinstallNeed(currentMode, recordedEntrypoint, latestEntrypoint, latestShim, candidateStatuses, loaded.Path, installStatePath)

	return vscodeDetectResponse{
		SSHSession:                 admin.sshSession,
		RecommendedMode:            recommendedMode,
		CurrentMode:                displayVSCodeMode(currentMode),
		CurrentBinary:              currentBinary,
		InstallStatePath:           installStatePath,
		InstallState:               installState,
		Settings:                   settingsStatus,
		CandidateBundleEntrypoints: defaults.CandidateBundleEntrypoints,
		LatestBundleEntrypoint:     latestEntrypoint,
		RecordedBundleEntrypoint:   recordedEntrypoint,
		LatestShim:                 latestShim,
		RecordedShim:               recordedShim,
		NeedsShimReinstall:         needsReinstall,
	}, nil
}

func (a *App) applyVSCodeIntegration(req vscodeApplyRequest) error {
	defaults, err := a.platformDefaults()
	if err != nil {
		return err
	}
	currentBinary, err := a.currentBinaryPath()
	if err != nil {
		return err
	}
	mode, err := resolveVSCodeMode(strings.TrimSpace(req.Mode))
	if err != nil {
		return err
	}

	statePath := a.installStatePath()
	state, err := loadInstallStateIfPresent(statePath)
	if err != nil {
		return err
	}
	if state == nil {
		state = &install.InstallState{
			StatePath: statePath,
		}
	}
	install.ApplyStateMetadata(state, install.StateMetadataOptions{
		StatePath:       statePath,
		InstalledBinary: currentBinary,
		CurrentVersion:  a.currentBinaryVersion(),
	})
	if reqSettings := strings.TrimSpace(req.SettingsPath); reqSettings != "" {
		state.VSCodeSettingsPath = normalizeVSCodeSettingsPathForState(reqSettings, defaults.VSCodeSettingsPath)
	} else {
		state.VSCodeSettingsPath = normalizeVSCodeSettingsPathForState(state.VSCodeSettingsPath, defaults.VSCodeSettingsPath)
	}
	bundleEntrypoint := strings.TrimSpace(req.BundleEntrypoint)
	if bundleEntrypoint == "" && len(defaults.CandidateBundleEntrypoints) > 0 {
		bundleEntrypoint = defaults.CandidateBundleEntrypoints[0]
	}
	if bundleEntrypoint == "" {
		bundleEntrypoint = strings.TrimSpace(state.BundleEntrypoint)
	}
	if modeIncludes(mode, install.IntegrationManagedShim) {
		if strings.TrimSpace(bundleEntrypoint) == "" {
			return errors.New("no vscode extension bundle entrypoint detected")
		}
		configPath := loadedConfigPath(a)
		statuses, err := detectManagedShimStatuses(defaults.CandidateBundleEntrypoints, currentBinary)
		if err != nil {
			return err
		}
		if recorded := strings.TrimSpace(state.BundleEntrypoint); recorded != "" {
			status, err := lookupManagedShimStatus(statuses, recorded, currentBinary)
			if err != nil {
				return err
			}
			statuses[recorded] = status
		}
		for _, target := range managedShimMigrationTargets(bundleEntrypoint, state.BundleEntrypoint, statuses, configPath, statePath) {
			if err := editor.PatchBundleEntrypoint(editor.PatchBundleEntrypointOptions{
				EntrypointPath:   target,
				InstallStatePath: statePath,
				ConfigPath:       configPath,
				InstanceID:       state.InstanceID,
			}); err != nil {
				return err
			}
		}
		if err := editor.ClearVSCodeSettingsExecutable(xutil.FirstNonEmpty(strings.TrimSpace(state.VSCodeSettingsPath), defaults.VSCodeSettingsPath)); err != nil {
			return err
		}
		state.BundleEntrypoint = bundleEntrypoint
	}

	if err := a.updateVSCodeConfig(mode, bundleEntrypoint); err != nil {
		return err
	}

	install.ApplyStateMetadata(state, install.StateMetadataOptions{
		StatePath:       statePath,
		InstalledBinary: currentBinary,
		CurrentVersion:  a.currentBinaryVersion(),
		InstanceID:      state.InstanceID,
	})
	state.ConfigPath = loadedConfigPath(a)
	state.CurrentBinaryPath = currentBinary
	state.StatePath = statePath
	if err := install.WriteState(statePath, *state); err != nil {
		return err
	}
	return nil
}

func (a *App) reinstallVSCodeShim(bundleEntrypoint string) error {
	defaults, err := a.platformDefaults()
	if err != nil {
		return err
	}
	currentBinary, err := a.currentBinaryPath()
	if err != nil {
		return err
	}
	statePath := a.installStatePath()
	state, err := loadInstallStateIfPresent(statePath)
	if err != nil {
		return err
	}
	if state == nil {
		state = &install.InstallState{StatePath: statePath}
	}
	install.ApplyStateMetadata(state, install.StateMetadataOptions{
		StatePath:       statePath,
		InstalledBinary: currentBinary,
		CurrentVersion:  a.currentBinaryVersion(),
	})
	target := strings.TrimSpace(bundleEntrypoint)
	if target == "" && len(defaults.CandidateBundleEntrypoints) > 0 {
		target = defaults.CandidateBundleEntrypoints[0]
	}
	if target == "" {
		target = strings.TrimSpace(state.BundleEntrypoint)
	}
	if target == "" {
		return errors.New("no vscode extension bundle entrypoint detected")
	}
	configPath := loadedConfigPath(a)
	statuses, err := detectManagedShimStatuses(defaults.CandidateBundleEntrypoints, currentBinary)
	if err != nil {
		return err
	}
	if recorded := strings.TrimSpace(state.BundleEntrypoint); recorded != "" {
		status, err := lookupManagedShimStatus(statuses, recorded, currentBinary)
		if err != nil {
			return err
		}
		statuses[recorded] = status
	}
	for _, candidate := range managedShimMigrationTargets(target, state.BundleEntrypoint, statuses, configPath, statePath) {
		if err := editor.PatchBundleEntrypoint(editor.PatchBundleEntrypointOptions{
			EntrypointPath:   candidate,
			InstallStatePath: statePath,
			ConfigPath:       configPath,
			InstanceID:       state.InstanceID,
		}); err != nil {
			return err
		}
	}
	settingsPath := xutil.FirstNonEmpty(strings.TrimSpace(state.VSCodeSettingsPath), defaults.VSCodeSettingsPath)
	if err := editor.ClearVSCodeSettingsExecutable(settingsPath); err != nil {
		return err
	}
	state.VSCodeSettingsPath = normalizeVSCodeSettingsPathForState(settingsPath, defaults.VSCodeSettingsPath)
	if err := a.updateVSCodeConfig(string(install.IntegrationManagedShim), target); err != nil {
		return err
	}
	install.ApplyStateMetadata(state, install.StateMetadataOptions{
		StatePath:       statePath,
		InstalledBinary: currentBinary,
		CurrentVersion:  a.currentBinaryVersion(),
		InstanceID:      state.InstanceID,
	})
	state.BundleEntrypoint = target
	state.CurrentBinaryPath = currentBinary
	state.StatePath = statePath
	if err := install.WriteState(statePath, *state); err != nil {
		return err
	}
	return nil
}

func (a *App) updateVSCodeConfig(mode, bundleEntrypoint string) error {
	a.adminConfigMu.Lock()
	defer a.adminConfigMu.Unlock()
	_ = bundleEntrypoint

	loaded, err := a.loadAdminConfig()
	if err != nil {
		return err
	}
	cfg := loaded.Config
	cfg.Wrapper.IntegrationMode = mode
	return config.WriteAppConfig(loaded.Path, cfg)
}

func (a *App) currentBinaryPath() (string, error) {
	if strings.TrimSpace(a.headlessRuntime.BinaryPath) != "" {
		return a.headlessRuntime.BinaryPath, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err == nil {
		return resolved, nil
	}
	return executable, nil
}

func (a *App) installStatePath() string {
	if strings.TrimSpace(a.headlessRuntime.Paths.DataDir) != "" {
		return filepath.Join(a.headlessRuntime.Paths.DataDir, "install-state.json")
	}
	paths, err := relayruntime.DefaultPaths()
	if err != nil {
		return filepath.Join(".", "install-state.json")
	}
	return filepath.Join(paths.DataDir, "install-state.json")
}

func loadInstallStateIfPresent(path string) (*install.InstallState, error) {
	state, err := install.LoadState(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &state, nil
}

func resolveVSCodeMode(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return string(install.IntegrationManagedShim), nil
	}
	switch raw {
	case string(install.IntegrationManagedShim):
		return string(install.IntegrationManagedShim), nil
	default:
		return "", errors.New("unsupported vscode integration mode")
	}
}

func displayVSCodeMode(mode string) string {
	return strings.TrimSpace(mode)
}

func normalizeVSCodeSettingsPathForState(path, defaultPath string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if samePlatformPath(path, defaultPath) {
		return ""
	}
	return path
}

func modeIncludes(mode string, target install.WrapperIntegrationMode) bool {
	return mode == string(target)
}

func samePlatformPath(left, right string) bool {
	return pathcompare.SameCleanPlatformPath(left, right)
}

func loadedConfigPath(a *App) string {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return ""
	}
	return loaded.Path
}

func (a *App) platformDefaults() (install.PlatformDefaults, error) {
	if a.detectPlatformDefaults != nil {
		return a.detectPlatformDefaults()
	}
	return install.DetectPlatformDefaults()
}

func (a *App) currentBinaryVersion() string {
	return strings.TrimSpace(a.serverIdentity.Version)
}
