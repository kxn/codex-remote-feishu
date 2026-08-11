package vscodeshim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/shim"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
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
	realBinaryPath := shim.RealBinaryPath(entrypointPath)
	sidecarPath := shim.SidecarPath(entrypointPath)

	sidecar, err := shim.ReadSidecar(sidecarPath)
	if err == nil && shim.SidecarValid(sidecar, shim.ModeManaged) {
		state, loadErr := loadInstallState(sidecar.InstallStatePath)
		if loadErr == nil {
			targetBinary := strings.TrimSpace(state.CurrentBinaryPath)
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
	state.ConfigPath = xutil.CleanPath(state.ConfigPath)
	state.InstalledBinary = xutil.CleanPath(state.InstalledBinary)
	state.InstalledWrapperBinary = xutil.CleanPath(state.InstalledWrapperBinary)
	// Legacy states recorded the installed binary under installedBinary /
	// installedWrapperBinary; promote to the canonical current-binary path.
	state.CurrentBinaryPath = firstNonEmpty(
		xutil.CleanPath(state.CurrentBinaryPath),
		state.InstalledBinary,
		state.InstalledWrapperBinary,
	)
	return state, nil
}

func usableConfigPath(path string) bool {
	path = xutil.CleanPath(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func usableLaunchTarget(path, entrypointPath, realBinaryPath string) bool {
	path = xutil.CleanPath(path)
	if path == "" {
		return false
	}
	if shim.SamePath(path, entrypointPath) || shim.SamePath(path, realBinaryPath) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func usableFallbackTarget(path string) bool {
	path = xutil.CleanPath(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func withManagedShimEnv(baseEnv []string, configPath, realBinaryPath string) []string {
	env := append([]string(nil), baseEnv...)
	env = upsertEnv(env, "CODEX_REMOTE_CONFIG", xutil.CleanPath(configPath))
	env = upsertEnv(env, "CODEX_REAL_BINARY", xutil.CleanPath(realBinaryPath))
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
		if cleaned := xutil.CleanPath(value); cleaned != "" {
			return cleaned
		}
	}
	return ""
}
