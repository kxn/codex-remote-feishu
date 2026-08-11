// Package shim holds the single-source sidecar model and role dispatch shared
// by the unified shim binary (cmd/shim). It replaces the former duplicated
// internal/managedshim and internal/upgradeshim packages: sidecar JSON schema,
// validation, read/write, and normalization now live in one place, and the
// managed/upgrade differences are parameterized by Mode.
package shim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

// Mode selects the shim role a unified binary takes when invoked.
type Mode int

const (
	// ModeManaged is the daemon-managed VS Code entrypoint shim (the old
	// vscode-shim / managedshim role): it proxies the real binary and records
	// install-state + config paths in its sidecar.
	ModeManaged Mode = iota
	// ModeUpgrade is the upgrade transaction helper shim (the old
	// upgrade-shim / upgradeshim role): it reads its sidecar to locate the
	// install-state and runs the upgrade transaction.
	ModeUpgrade
)

// SidecarManager is the sidecar manager label recorded for managed shims.
const SidecarManager = "codex-remote"

// UpgradeSidecarManager is the sidecar manager label recorded for upgrade shims.
const UpgradeSidecarManager = "codex-remote-upgrade"

// SidecarSchemaVersion is the current sidecar JSON schema version.
const SidecarSchemaVersion = 1

// Manager returns the sidecar manager label for the mode.
func (m Mode) Manager() string {
	if m == ModeUpgrade {
		return UpgradeSidecarManager
	}
	return SidecarManager
}

// Sidecar is the on-disk metadata written next to a shim entrypoint as
// <entrypoint>.remote.json. It is the single schema for both managed and
// upgrade shims; managed shims additionally record ConfigPath.
type Sidecar struct {
	SchemaVersion    int    `json:"schemaVersion"`
	Manager          string `json:"manager"`
	InstallStatePath string `json:"installStatePath"`
	ConfigPath       string `json:"configPath,omitempty"`
	InstanceID       string `json:"instanceId,omitempty"`
}

// RealBinaryPath returns the path of the renamed original binary next to a
// managed shim entrypoint (e.g. /tmp/codex -> /tmp/codex.real).
func RealBinaryPath(entrypointPath string) string {
	ext := filepath.Ext(entrypointPath)
	if ext == "" {
		return entrypointPath + ".real"
	}
	return strings.TrimSuffix(entrypointPath, ext) + ".real" + ext
}

// SidecarPath returns the sidecar path for a shim entrypoint
// (e.g. /tmp/codex -> /tmp/codex.remote.json).
func SidecarPath(entrypointPath string) string {
	ext := filepath.Ext(entrypointPath)
	if ext == "" {
		return entrypointPath + ".remote.json"
	}
	return strings.TrimSuffix(entrypointPath, ext) + ".remote.json"
}

// NormalizeSidecar fills mode-derived fields and cleans path fields.
func NormalizeSidecar(sidecar Sidecar, mode Mode) Sidecar {
	sidecar.SchemaVersion = SidecarSchemaVersion
	sidecar.Manager = mode.Manager()
	sidecar.InstallStatePath = xutil.CleanPath(sidecar.InstallStatePath)
	sidecar.ConfigPath = xutil.CleanPath(sidecar.ConfigPath)
	sidecar.InstanceID = strings.TrimSpace(sidecar.InstanceID)
	return sidecar
}

// SidecarValid reports whether the sidecar carries the bindings required for
// the mode: managed shims need install-state + config paths; upgrade shims
// only need the install-state path.
func SidecarValid(sidecar Sidecar, mode Mode) bool {
	sidecar = NormalizeSidecar(sidecar, mode)
	if sidecar.InstallStatePath == "" {
		return false
	}
	if mode == ModeManaged && sidecar.ConfigPath == "" {
		return false
	}
	return true
}

// ModeForManager maps a sidecar manager label to the shim mode that owns it.
// Anything other than the upgrade manager label is treated as managed, which
// keeps unknown/legacy labels on the managed fallback path.
func ModeForManager(manager string) Mode {
	if strings.TrimSpace(manager) == UpgradeSidecarManager {
		return ModeUpgrade
	}
	return ModeManaged
}

// ReadSidecar reads and normalizes a sidecar file, inferring the mode from the
// stored manager label so managed and upgrade sidecars written by any version
// are both handled identically.
func ReadSidecar(path string) (Sidecar, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Sidecar{}, err
	}
	var sidecar Sidecar
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		return Sidecar{}, err
	}
	return NormalizeSidecar(sidecar, ModeForManager(sidecar.Manager)), nil
}

// WriteSidecar validates and atomically writes a sidecar for the given mode.
func WriteSidecar(path string, sidecar Sidecar, mode Mode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("sidecar path is required")
	}
	sidecar = NormalizeSidecar(sidecar, mode)
	if !SidecarValid(sidecar, mode) {
		if mode == ModeManaged {
			return fmt.Errorf("managed shim sidecar requires installStatePath and configPath")
		}
		return fmt.Errorf("upgrade shim sidecar requires installStatePath")
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

// SamePath reports whether two cleaned paths refer to the same file, using
// case-insensitive comparison on Windows.
func SamePath(left, right string) bool {
	left = xutil.CleanPath(left)
	right = xutil.CleanPath(right)
	if left == "" || right == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
