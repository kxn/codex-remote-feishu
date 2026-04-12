package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		return InstallState{}, err
	}
	state := disk.InstallState
	state.StatePath = firstNonEmpty(strings.TrimSpace(state.StatePath), strings.TrimSpace(path))
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
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
