package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/pathscope"
)

type PlatformDefaults struct {
	GOOS                       string
	HomeDir                    string
	BaseDir                    string
	InstallBinDir              string
	VSCodeSettingsPath         string
	CandidateBundleEntrypoints []string
	DefaultIntegrations        []WrapperIntegrationMode
}

func DetectPlatformDefaults() (PlatformDefaults, error) {
	homeDir, err := pathscope.UserHomeDir()
	if err != nil {
		return PlatformDefaults{}, err
	}
	goos := runtime.GOOS
	return PlatformDefaults{
		GOOS:                       goos,
		HomeDir:                    homeDir,
		BaseDir:                    homeDir,
		InstallBinDir:              defaultInstallBinDir(goos, homeDir),
		VSCodeSettingsPath:         defaultVSCodeSettingsPath(goos, homeDir),
		CandidateBundleEntrypoints: detectBundleEntrypointsFunc(goos, runtime.GOARCH, homeDir),
		DefaultIntegrations:        DefaultIntegrations(goos),
	}, nil
}

func defaultInstallBinDir(goos, homeDir string) string {
	return defaultInstallBinDirForInstance(goos, homeDir, defaultInstanceID)
}

func defaultInstallBinDirForInstance(goos, homeDir, instanceID string) string {
	switch goos {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", instanceNamespace(instanceID), "bin")
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); strings.TrimSpace(localAppData) != "" {
			return filepath.Join(pathscope.ApplyPrefix(localAppData), instanceNamespace(instanceID), "bin")
		}
	}
	return filepath.Join(homeDir, ".local", "share", instanceNamespace(instanceID), "bin")
}

func defaultVSCodeSettingsPath(goos, homeDir string) string {
	switch goos {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", "Code", "User", "settings.json")
	case "windows":
		if appData := os.Getenv("APPDATA"); strings.TrimSpace(appData) != "" {
			return filepath.Join(pathscope.ApplyPrefix(appData), "Code", "User", "settings.json")
		}
	}
	return filepath.Join(homeDir, ".config", "Code", "User", "settings.json")
}

func detectBundleEntrypoints(goos, goarch, homeDir string) []string {
	return detectBundleEntrypointsFunc(goos, goarch, homeDir)
}

func recommendedBundleEntrypoint(defaults PlatformDefaults) string {
	if len(defaults.CandidateBundleEntrypoints) == 0 {
		return ""
	}
	return defaults.CandidateBundleEntrypoints[0]
}

func integrationHelpText(goos string) string {
	return strings.TrimSpace(fmt.Sprintf(`
1. managed_shim
   当前唯一推荐的 VS Code 接入方式。安装器会直接替换扩展 bundle 里的 codex 入口，并保留原始 codex.real。
   这不会修改客户端侧 settings.json，因此不会把 host 机器上的 override 带进 Remote SSH 会话。

当前平台默认：
- %s
`, integrationsConfigValue(DefaultIntegrations(goos))))
}
