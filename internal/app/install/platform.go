package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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
		CandidateBundleEntrypoints: detectBundleEntrypoints(goos, runtime.GOARCH, homeDir),
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
			return filepath.Join(localAppData, instanceNamespace(instanceID), "bin")
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
			return filepath.Join(appData, "Code", "User", "settings.json")
		}
	}
	return filepath.Join(homeDir, ".config", "Code", "User", "settings.json")
}

func detectBundleEntrypoints(goos, goarch, homeDir string) []string {
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
			info, err := dir.Info()
			if err != nil {
				continue
			}
			entrypoint := detectExtensionBundleEntrypoint(goos, goarch, extensionDir)
			if strings.TrimSpace(entrypoint) != "" {
				found = append(found, candidate{path: entrypoint, modTime: info.ModTime().UnixNano()})
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

func detectExtensionBundleEntrypoint(goos, goarch, extensionDir string) string {
	binDir := filepath.Join(extensionDir, "bin")
	dirs, err := os.ReadDir(binDir)
	if err != nil {
		return ""
	}

	type candidate struct {
		path  string
		score int
	}
	found := make([]candidate, 0, len(dirs))
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		score, ok := bundlePlatformDirScore(goos, goarch, dir.Name())
		if !ok {
			continue
		}
		entrypoint := detectBundleEntrypointFile(goos, filepath.Join(binDir, dir.Name()))
		if strings.TrimSpace(entrypoint) == "" {
			continue
		}
		found = append(found, candidate{path: entrypoint, score: score})
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].score == found[j].score {
			return strings.Compare(found[i].path, found[j].path) < 0
		}
		return found[i].score > found[j].score
	})
	if len(found) == 0 {
		return ""
	}
	return found[0].path
}

func detectBundleEntrypointFile(goos, bundleDir string) string {
	for _, name := range bundleEntrypointNames(goos) {
		path := filepath.Join(bundleDir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		return path
	}
	return ""
}

func bundleEntrypointNames(goos string) []string {
	switch goos {
	case "windows":
		return []string{"codex.exe", "codex"}
	default:
		return []string{"codex", "codex.exe"}
	}
}

func bundlePlatformDirScore(goos, goarch, dirName string) (int, bool) {
	name := strings.ToLower(strings.TrimSpace(dirName))
	if name == "" {
		return 0, false
	}

	remainder, ok := trimBundlePlatformPrefix(name, goos)
	if !ok {
		return 0, false
	}
	if remainder == "" {
		return 100, true
	}

	tokens := strings.Split(remainder, "-")
	for index, alias := range platformArchAliases(goarch) {
		for _, token := range tokens {
			if token == alias {
				return 300 - index, true
			}
		}
	}
	if goos == "darwin" {
		for _, token := range tokens {
			if token == "universal" {
				return 200, true
			}
		}
	}
	return 0, false
}

func trimBundlePlatformPrefix(name, goos string) (string, bool) {
	for _, alias := range platformOSAliases(goos) {
		if name == alias {
			return "", true
		}
		prefix := alias + "-"
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, prefix), true
		}
	}
	return "", false
}

func platformOSAliases(goos string) []string {
	switch goos {
	case "windows":
		return []string{"windows", "win32"}
	case "darwin":
		return []string{"darwin", "macos"}
	default:
		return []string{goos}
	}
}

func platformArchAliases(goarch string) []string {
	switch strings.ToLower(strings.TrimSpace(goarch)) {
	case "amd64":
		return []string{"x86_64", "x64", "amd64"}
	case "arm64":
		return []string{"arm64", "aarch64"}
	case "386":
		return []string{"x86", "ia32", "386"}
	default:
		normalized := strings.ToLower(strings.TrimSpace(goarch))
		if normalized == "" {
			return nil
		}
		return []string{normalized}
	}
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
