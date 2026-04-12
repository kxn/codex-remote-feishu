package daemon

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/editor"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/config"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
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

	recordedEntrypoint := ""
	var recordedShim *editor.ManagedShimStatus
	if installState != nil && strings.TrimSpace(installState.BundleEntrypoint) != "" {
		recordedEntrypoint = installState.BundleEntrypoint
		status, err := lookupManagedShimStatus(candidateStatuses, recordedEntrypoint, currentBinary)
		if err != nil {
			return vscodeDetectResponse{}, err
		}
		candidateStatuses[recordedEntrypoint] = status
		recordedShim = &status
	}

	currentMode := strings.TrimSpace(loaded.Config.Wrapper.IntegrationMode)
	if currentMode == "" {
		currentMode = string(install.IntegrationManagedShim)
	}
	recommendedMode := string(install.IntegrationManagedShim)
	_ = admin
	needsReinstall := computeShimReinstallNeed(currentMode, installState, latestEntrypoint, latestShim, candidateStatuses, loaded.Path, installStatePath)

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
	mode, err := resolveVSCodeMode(strings.TrimSpace(req.Mode), a.snapshotAdminRuntime().sshSession)
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
	if strings.TrimSpace(state.VSCodeSettingsPath) == "" {
		state.VSCodeSettingsPath = firstNonEmpty(strings.TrimSpace(req.SettingsPath), defaults.VSCodeSettingsPath)
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
		if err := editor.ClearVSCodeSettingsExecutable(state.VSCodeSettingsPath); err != nil {
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
	state.InstalledBinary = currentBinary
	state.InstalledWrapperBinary = currentBinary
	state.InstalledRelaydBinary = currentBinary
	state.CurrentBinaryPath = currentBinary
	state.StatePath = statePath
	state.Integrations = integrationModesFor(mode)
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
	settingsPath := firstNonEmpty(strings.TrimSpace(state.VSCodeSettingsPath), defaults.VSCodeSettingsPath)
	if err := editor.ClearVSCodeSettingsExecutable(settingsPath); err != nil {
		return err
	}
	state.VSCodeSettingsPath = settingsPath
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
	state.InstalledBinary = currentBinary
	state.InstalledWrapperBinary = currentBinary
	state.InstalledRelaydBinary = currentBinary
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

func resolveVSCodeMode(raw string, sshSession bool) (string, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return string(install.IntegrationManagedShim), nil
	}
	switch raw {
	case string(install.IntegrationManagedShim), "both", "all":
		return string(install.IntegrationManagedShim), nil
	case string(install.IntegrationEditorSettings):
		return "", errors.New("editor_settings integration is no longer supported; migrate to managed_shim instead")
	default:
		return "", errors.New("unsupported vscode integration mode")
	}
}

func displayVSCodeMode(mode string) string {
	if strings.TrimSpace(mode) == "both" {
		return string(install.IntegrationManagedShim)
	}
	return mode
}

func integrationModesFor(mode string) []install.WrapperIntegrationMode {
	return []install.WrapperIntegrationMode{install.IntegrationManagedShim}
}

func modeIncludes(mode string, target install.WrapperIntegrationMode) bool {
	switch mode {
	case "both":
		return true
	default:
		return mode == string(target)
	}
}

func samePlatformPath(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
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
