package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/atomicfile"
	"github.com/kxn/codex-remote-feishu/internal/pathcompare"
	"github.com/kxn/codex-remote-feishu/internal/pathscope"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func LoadState(path string) (InstallState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return InstallState{}, err
	}
	var disk struct {
		InstallState
		WrapperConfigPath  string `json:"wrapperConfigPath"`
		ServicesConfigPath string `json:"servicesConfigPath"`
		InstalledBinary    string `json:"installedBinary"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		return InstallState{}, err
	}
	state := disk.InstallState
	state.StatePath = xutil.FirstNonEmpty(strings.TrimSpace(state.StatePath), strings.TrimSpace(path))
	// Legacy states recorded the installed binary under "installedBinary"
	// (and once under three per-role fields). Promote it to the canonical
	// CurrentBinaryPath so readers that only know the canonical field keep
	// working with old files.
	state.CurrentBinaryPath = xutil.FirstNonEmpty(strings.TrimSpace(state.CurrentBinaryPath), strings.TrimSpace(disk.InstalledBinary))
	state.ConfigPath = normalizeInstallStateConfigPath(
		state.ConfigPath,
		disk.WrapperConfigPath,
		disk.ServicesConfigPath,
		state.StatePath,
		state.BaseDir,
		state.InstanceID,
	)
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})
	return state, nil
}

func WriteState(path string, state InstallState) error {
	if err := pathscope.EnsureWritePath(path); err != nil {
		return err
	}
	// Layout facts (baseDir / configPath / statePath / versionsRoot) are
	// derived from the state file location at load time (LoadState +
	// ApplyStateMetadata). Persisting snapshots of them is the stale-fact
	// source #808-C removes: if the install moves or the files are edited, the
	// snapshot lies while the derivation stays correct. currentSlot is kept:
	// it identifies the active version slot and cannot be derived from the
	// state path alone (the live binary lives in the install bin dir, not
	// under versionsRoot). Write only what cannot be derived; the caller's
	// in-memory state is left untouched.
	persisted := state
	persisted.BaseDir = ""
	persisted.StatePath = ""
	persisted.VersionsRoot = ""
	if pathcompare.SameCleanPlatformPath(persisted.ConfigPath, defaultConfigPathForState(path)) {
		persisted.ConfigPath = ""
	}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := atomicfile.Write(path, raw, 0o644); err != nil {
		return err
	}
	return nil
}

// defaultConfigPathForState derives the default config.json path for the
// install whose state file lives at statePath. It returns "" when the layout
// cannot be derived from the path alone.
func defaultConfigPathForState(statePath string) string {
	baseDir, instanceID, ok := inferBaseDirAndInstanceFromStatePath(statePath)
	if !ok {
		return ""
	}
	return defaultConfigPathForInstance(baseDir, instanceID)
}

func normalizeInstallStateConfigPath(configPath, wrapperConfigPath, servicesConfigPath, statePath, baseDir, instanceID string) string {
	for _, candidate := range []string{configPath, wrapperConfigPath, servicesConfigPath} {
		if normalized := normalizeInstallStateConfigPathValue(candidate); normalized != "" {
			return normalized
		}
	}
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = inferBaseDir("", statePath)
	}
	if baseDir == "" {
		return ""
	}
	if strings.TrimSpace(instanceID) == "" {
		instanceID = inferInstanceID("", statePath)
	}
	return defaultConfigPathForInstance(baseDir, normalizeInstanceID(instanceID))
}

func normalizeInstallStateConfigPathValue(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if isLegacyInstallStateConfigPath(cleaned) {
		return filepath.Join(filepath.Dir(cleaned), "config.json")
	}
	return cleaned
}

func isLegacyInstallStateConfigPath(path string) bool {
	switch filepath.Base(strings.TrimSpace(path)) {
	case "config.env", "wrapper.env", "services.env":
		return true
	default:
		return false
	}
}
