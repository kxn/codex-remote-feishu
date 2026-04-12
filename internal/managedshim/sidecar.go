package managedshim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	SidecarSchemaVersion = 1
	SidecarManager       = "codex-remote"
)

type Sidecar struct {
	SchemaVersion    int    `json:"schemaVersion"`
	Manager          string `json:"manager"`
	InstallStatePath string `json:"installStatePath"`
	ConfigPath       string `json:"configPath"`
	InstanceID       string `json:"instanceId,omitempty"`
}

func RealBinaryPath(entrypointPath string) string {
	ext := filepath.Ext(entrypointPath)
	if ext == "" {
		return entrypointPath + ".real"
	}
	return strings.TrimSuffix(entrypointPath, ext) + ".real" + ext
}

func SidecarPath(entrypointPath string) string {
	ext := filepath.Ext(entrypointPath)
	if ext == "" {
		return entrypointPath + ".remote.json"
	}
	return strings.TrimSuffix(entrypointPath, ext) + ".remote.json"
}

func NormalizeSidecar(sidecar Sidecar) Sidecar {
	sidecar.SchemaVersion = SidecarSchemaVersion
	sidecar.Manager = SidecarManager
	sidecar.InstallStatePath = cleanNonEmpty(sidecar.InstallStatePath)
	sidecar.ConfigPath = cleanNonEmpty(sidecar.ConfigPath)
	sidecar.InstanceID = strings.TrimSpace(sidecar.InstanceID)
	return sidecar
}

func SidecarValid(sidecar Sidecar) bool {
	sidecar = NormalizeSidecar(sidecar)
	return sidecar.InstallStatePath != "" && sidecar.ConfigPath != ""
}

func ReadSidecar(path string) (Sidecar, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Sidecar{}, err
	}
	var sidecar Sidecar
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		return Sidecar{}, err
	}
	return NormalizeSidecar(sidecar), nil
}

func WriteSidecar(path string, sidecar Sidecar) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("sidecar path is required")
	}
	sidecar = NormalizeSidecar(sidecar)
	if !SidecarValid(sidecar) {
		return fmt.Errorf("managed shim sidecar requires installStatePath and configPath")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func SamePath(left, right string) bool {
	left = cleanNonEmpty(left)
	right = cleanNonEmpty(right)
	if left == "" || right == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func cleanNonEmpty(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}
