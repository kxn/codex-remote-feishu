package vscodeshim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/managedshim"
)

type launchPlan struct {
	BinaryPath string
	Env        []string
	Fallback   bool
}

type installState struct {
	ConfigPath             string `json:"configPath,omitempty"`
	CurrentBinaryPath      string `json:"currentBinaryPath,omitempty"`
	InstalledBinary        string `json:"installedBinary,omitempty"`
	InstalledWrapperBinary string `json:"installedWrapperBinary,omitempty"`
}

func RunMain(args []string) int {
	executable, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "vscode shim: resolve executable: %v\n", err)
		return 1
	}
	plan, err := resolveLaunchPlan(executable, os.Environ())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "vscode shim: %v\n", err)
		return 1
	}
	if err := execBinary(plan.BinaryPath, args, plan.Env); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "vscode shim exec failed: %v\n", err)
		return 1
	}
	return 0
}

func resolveLaunchPlan(entrypointPath string, baseEnv []string) (launchPlan, error) {
	entrypointPath = filepath.Clean(strings.TrimSpace(entrypointPath))
	if entrypointPath == "" {
		return launchPlan{}, fmt.Errorf("entrypoint path is empty")
	}
	realBinaryPath := managedshim.RealBinaryPath(entrypointPath)
	sidecarPath := managedshim.SidecarPath(entrypointPath)

	sidecar, err := managedshim.ReadSidecar(sidecarPath)
	if err == nil && managedshim.SidecarValid(sidecar) {
		state, loadErr := loadInstallState(sidecar.InstallStatePath)
		if loadErr == nil {
			targetBinary := firstNonEmpty(
				state.CurrentBinaryPath,
				state.InstalledBinary,
				state.InstalledWrapperBinary,
			)
			configPath := firstNonEmpty(sidecar.ConfigPath, state.ConfigPath)
			if usableConfigPath(configPath) && usableLaunchTarget(targetBinary, entrypointPath, realBinaryPath) {
				env := withManagedShimEnv(baseEnv, configPath, realBinaryPath)
				return launchPlan{
					BinaryPath: targetBinary,
					Env:        env,
				}, nil
			}
		}
	}

	if usableFallbackTarget(realBinaryPath) {
		return launchPlan{
			BinaryPath: realBinaryPath,
			Env:        baseEnv,
			Fallback:   true,
		}, nil
	}
	return launchPlan{}, fmt.Errorf("no valid managed target or fallback codex.real found for %s", entrypointPath)
}

func loadInstallState(path string) (installState, error) {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return installState{}, err
	}
	var state installState
	if err := json.Unmarshal(raw, &state); err != nil {
		return installState{}, err
	}
	state.ConfigPath = cleanNonEmpty(state.ConfigPath)
	state.CurrentBinaryPath = cleanNonEmpty(state.CurrentBinaryPath)
	state.InstalledBinary = cleanNonEmpty(state.InstalledBinary)
	state.InstalledWrapperBinary = cleanNonEmpty(state.InstalledWrapperBinary)
	return state, nil
}

func usableConfigPath(path string) bool {
	path = cleanNonEmpty(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func usableLaunchTarget(path, entrypointPath, realBinaryPath string) bool {
	path = cleanNonEmpty(path)
	if path == "" {
		return false
	}
	if managedshim.SamePath(path, entrypointPath) || managedshim.SamePath(path, realBinaryPath) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func usableFallbackTarget(path string) bool {
	path = cleanNonEmpty(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func withManagedShimEnv(baseEnv []string, configPath, realBinaryPath string) []string {
	env := append([]string(nil), baseEnv...)
	env = upsertEnv(env, "CODEX_REMOTE_CONFIG", cleanNonEmpty(configPath))
	env = upsertEnv(env, "CODEX_REAL_BINARY", cleanNonEmpty(realBinaryPath))
	return env
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	replaced := false
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			if !replaced {
				result = append(result, prefix+value)
				replaced = true
			}
			continue
		}
		result = append(result, item)
	}
	if !replaced {
		result = append(result, prefix+value)
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if cleaned := cleanNonEmpty(value); cleaned != "" {
			return cleaned
		}
	}
	return ""
}

func cleanNonEmpty(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}
