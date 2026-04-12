package install

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	defaultInstanceID = "stable"
	debugInstanceID   = "debug"
	productName       = "codex-remote"
)

var instanceIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,61}[a-z0-9])?$`)

type instancePaths struct {
	ConfigHome string
	DataHome   string
	StateHome  string
}

func normalizeInstanceID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", defaultInstanceID:
		return defaultInstanceID
	default:
		return value
	}
}

func parseInstanceID(value string) (string, error) {
	instanceID := normalizeInstanceID(value)
	if isDefaultInstance(instanceID) {
		return instanceID, nil
	}
	if !instanceIDPattern.MatchString(instanceID) {
		return "", fmt.Errorf("unsupported instance %q", strings.TrimSpace(value))
	}
	return instanceID, nil
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

func instanceIDFromNamespace(namespace string) (string, bool) {
	namespace = strings.TrimSpace(namespace)
	if namespace == productName {
		return defaultInstanceID, true
	}
	prefix := productName + "-"
	if !strings.HasPrefix(namespace, prefix) {
		return "", false
	}
	instanceID, err := parseInstanceID(strings.TrimPrefix(namespace, prefix))
	if err != nil || isDefaultInstance(instanceID) {
		return "", false
	}
	return instanceID, true
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
	if instanceID, ok := instanceIDFromNamespace(filepath.Base(configHome)); ok {
		parent := filepath.Dir(configHome)
		if filepath.Base(parent) != ".config" {
			return "", "", false
		}
		return filepath.Dir(parent), instanceID, true
	}
	return "", "", false
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
	if instanceID, ok := instanceIDFromNamespace(filepath.Base(dataHome)); ok {
		parent := filepath.Dir(dataHome)
		if filepath.Base(parent) != "share" {
			return "", "", false
		}
		localHome := filepath.Dir(parent)
		if filepath.Base(localHome) != ".local" {
			return "", "", false
		}
		return filepath.Dir(localHome), instanceID, true
	}
	return "", "", false
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
