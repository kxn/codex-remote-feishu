package install

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	defaultInstanceID = "stable"
	debugInstanceID   = "debug"
	productName       = "codex-remote"
)

type instancePaths struct {
	ConfigHome string
	DataHome   string
	StateHome  string
}

func normalizeInstanceID(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", defaultInstanceID:
		return defaultInstanceID
	case debugInstanceID:
		return debugInstanceID
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func parseInstanceID(value string) (string, error) {
	instanceID := normalizeInstanceID(value)
	switch instanceID {
	case defaultInstanceID, debugInstanceID:
		return instanceID, nil
	default:
		return "", fmt.Errorf("unsupported instance %q (want stable or debug)", strings.TrimSpace(value))
	}
}

func isDefaultInstance(instanceID string) bool {
	return normalizeInstanceID(instanceID) == defaultInstanceID
}

func systemdUserServiceNameForInstance(instanceID string) string {
	instanceID = normalizeInstanceID(instanceID)
	if isDefaultInstance(instanceID) {
		return productName + ".service"
	}
	return fmt.Sprintf("%s-%s.service", productName, instanceID)
}

func instanceNamespace(instanceID string) string {
	instanceID = normalizeInstanceID(instanceID)
	if isDefaultInstance(instanceID) {
		return productName
	}
	return fmt.Sprintf("%s-%s", productName, instanceID)
}

func instancePathsForBaseDir(baseDir, instanceID string) instancePaths {
	baseDir = filepath.Clean(strings.TrimSpace(baseDir))
	configHome := filepath.Join(baseDir, ".config")
	dataHome := filepath.Join(baseDir, ".local", "share")
	stateHome := filepath.Join(baseDir, ".local", "state")
	if isDefaultInstance(instanceID) {
		return instancePaths{
			ConfigHome: configHome,
			DataHome:   dataHome,
			StateHome:  stateHome,
		}
	}
	namespace := instanceNamespace(instanceID)
	return instancePaths{
		ConfigHome: filepath.Join(configHome, namespace),
		DataHome:   filepath.Join(dataHome, namespace),
		StateHome:  filepath.Join(stateHome, namespace),
	}
}

func inferBaseDirAndInstanceFromConfigPath(path string) (string, string, bool) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return "", "", false
	}
	dir := filepath.Dir(path)
	if filepath.Base(dir) != productName {
		return "", "", false
	}
	configHome := filepath.Dir(dir)
	if filepath.Base(configHome) == ".config" {
		return filepath.Dir(configHome), defaultInstanceID, true
	}
	parent := filepath.Dir(configHome)
	if filepath.Base(parent) != ".config" {
		return "", "", false
	}
	name := filepath.Base(configHome)
	if name != instanceNamespace(debugInstanceID) {
		return "", "", false
	}
	return filepath.Dir(parent), debugInstanceID, true
}

func inferBaseDirAndInstanceFromStatePath(path string) (string, string, bool) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return "", "", false
	}
	dir := filepath.Dir(path)
	if filepath.Base(dir) != productName {
		return "", "", false
	}
	dataHome := filepath.Dir(dir)
	if filepath.Base(dataHome) == "share" {
		localHome := filepath.Dir(dataHome)
		if filepath.Base(localHome) != ".local" {
			return "", "", false
		}
		return filepath.Dir(localHome), defaultInstanceID, true
	}
	parent := filepath.Dir(dataHome)
	if filepath.Base(parent) != "share" {
		return "", "", false
	}
	localHome := filepath.Dir(parent)
	if filepath.Base(localHome) != ".local" {
		return "", "", false
	}
	if filepath.Base(dataHome) != instanceNamespace(debugInstanceID) {
		return "", "", false
	}
	return filepath.Dir(localHome), debugInstanceID, true
}

func inferInstanceID(configPath, statePath string) string {
	if _, instanceID, ok := inferBaseDirAndInstanceFromStatePath(statePath); ok {
		return instanceID
	}
	if _, instanceID, ok := inferBaseDirAndInstanceFromConfigPath(configPath); ok {
		return instanceID
	}
	return defaultInstanceID
}
