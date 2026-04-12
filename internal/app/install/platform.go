package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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
	homeDir, err := os.UserHomeDir()
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
		CandidateBundleEntrypoints: detectBundleEntrypoints(goos, homeDir),
		DefaultIntegrations:        DefaultIntegrations(goos),
	}, nil
}

func defaultInstallBinDir(goos, homeDir string) string {
	return defaultInstallBinDirForInstance(goos, homeDir, defaultInstanceID)
}

func defaultInstallBinDirForInstance(goos, homeDir, instanceID string) string {
	if !isDefaultInstance(instanceID) {
		switch goos {
		case "darwin":
			return filepath.Join(homeDir, "Library", "Application Support", instanceNamespace(instanceID), "bin")
		case "windows":
			if localAppData := os.Getenv("LOCALAPPDATA"); strings.TrimSpace(localAppData) != "" {
				return filepath.Join(localAppData, instanceNamespace(instanceID), "bin")
			}
		default:
			return filepath.Join(homeDir, ".local", "share", instanceNamespace(instanceID), "bin")
		}
	}
	switch goos {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", "codex-remote", "bin")
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); strings.TrimSpace(localAppData) != "" {
			return filepath.Join(localAppData, "codex-remote", "bin")
		}
	}
	return filepath.Join(homeDir, ".local", "bin")
}

func defaultVSCodeSettingsPath(goos, homeDir string) string {
	switch goos {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", "Code", "User", "settings.json")
	case "windows":
		if appData := os.Getenv("APPDATA"); strings.TrimSpace(appData) != "" {
			return filepath.Join(appData, "Code", "User", "settings.json")
		}
	}
	return filepath.Join(homeDir, ".config", "Code", "User", "settings.json")
}

func detectBundleEntrypoints(goos, homeDir string) []string {
	var roots []string
	if envRoot := os.Getenv("VSCODE_SERVER_EXTENSIONS_DIR"); strings.TrimSpace(envRoot) != "" {
		roots = append(roots, envRoot)
	}
	switch goos {
	case "linux":
		roots = append(roots,
			filepath.Join(homeDir, ".vscode-server", "extensions"),
			filepath.Join(homeDir, ".vscode", "extensions"),
		)
	default:
		roots = append(roots, filepath.Join(homeDir, ".vscode", "extensions"))
	}

	type candidate struct {
		path    string
		modTime int64
	}
	var found []candidate
	for _, root := range roots {
		dirs, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, dir := range dirs {
			if !dir.IsDir() || !strings.HasPrefix(dir.Name(), "openai.chatgpt-") {
				continue
			}
			extensionDir := filepath.Join(root, dir.Name())
			matches, err := filepath.Glob(filepath.Join(extensionDir, "bin", "*", "*"))
			if err != nil {
				continue
			}
			info, err := dir.Info()
			if err != nil {
				continue
			}
			for _, match := range matches {
				base := strings.ToLower(filepath.Base(match))
				if base != "codex" && base != "codex.exe" {
					continue
				}
				found = append(found, candidate{path: match, modTime: info.ModTime().UnixNano()})
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].modTime == found[j].modTime {
			return strings.Compare(found[i].path, found[j].path) < 0
		}
		return found[i].modTime > found[j].modTime
	})
	seen := map[string]bool{}
	values := make([]string, 0, len(found))
	for _, item := range found {
		if seen[item.path] {
			continue
		}
		seen[item.path] = true
		values = append(values, item.path)
	}
	return values
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
