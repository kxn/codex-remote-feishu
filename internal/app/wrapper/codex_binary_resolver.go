package wrapper

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/editor"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

func resolveNormalCodexBinary(configPath, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return configured, nil
	}
	if strings.TrimSpace(os.Getenv("CODEX_REAL_BINARY")) != "" {
		return configured, nil
	}
	if !looksLikeVSCodeBundleCodexPath(configured) {
		return configured, nil
	}

	if _, err := exec.LookPath("codex"); err == nil {
		persistNormalCodexBinary(configPath, "codex")
		log.Printf("wrapper: shared normal codex binary self-healed from vscode bundle path %q to PATH codex", configured)
		return "codex", nil
	}

	if fallback := firstUsableVSCodeBundleCodex(configured); fallback != "" {
		persistNormalCodexBinary(configPath, "codex")
		log.Printf("wrapper: shared normal codex binary points to vscode bundle path %q; PATH codex unavailable, temporarily using vscode bundle codex %q", configured, fallback)
		return fallback, nil
	}

	return "", fmt.Errorf("shared normal codex binary points to vscode bundle path %q and no PATH codex or usable vscode bundle codex is available", configured)
}

func persistNormalCodexBinary(configPath, target string) {
	configPath = strings.TrimSpace(configPath)
	target = strings.TrimSpace(target)
	if configPath == "" || target == "" {
		return
	}
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		log.Printf("wrapper: normal codex self-heal skipped config rewrite for %q: %v", configPath, err)
		return
	}
	if strings.TrimSpace(loaded.Config.Wrapper.CodexRealBinary) == target {
		return
	}
	loaded.Config.Wrapper.CodexRealBinary = target
	if err := config.WriteAppConfig(configPath, loaded.Config); err != nil {
		log.Printf("wrapper: normal codex self-heal failed to rewrite %q: %v", configPath, err)
	}
}

func firstUsableVSCodeBundleCodex(configured string) string {
	candidates := []string{configured}
	candidates = append(candidates, siblingVSCodeBundleCodexCandidates(configured)...)
	if defaults, err := install.DetectPlatformDefaults(); err == nil {
		candidates = append(candidates, defaults.CandidateBundleEntrypoints...)
	}

	seen := map[string]bool{}
	for _, candidate := range candidates {
		resolved := usableVSCodeBundleCodexPath(candidate)
		if resolved == "" {
			continue
		}
		key := normalizedPathKey(resolved)
		if seen[key] {
			continue
		}
		seen[key] = true
		return resolved
	}
	return ""
}

func siblingVSCodeBundleCodexCandidates(configured string) []string {
	root := vscodeBundleExtensionsRoot(configured)
	if root == "" {
		return nil
	}

	type candidate struct {
		path    string
		modTime int64
	}

	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	found := make([]candidate, 0, len(dirs))
	for _, dir := range dirs {
		if !dir.IsDir() || !strings.HasPrefix(dir.Name(), "openai.chatgpt-") {
			continue
		}
		info, err := dir.Info()
		if err != nil {
			continue
		}
		for _, path := range vscodeBundleExtensionCandidates(filepath.Join(root, dir.Name())) {
			found = append(found, candidate{
				path:    path,
				modTime: info.ModTime().UnixNano(),
			})
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].modTime == found[j].modTime {
			return strings.Compare(found[i].path, found[j].path) < 0
		}
		return found[i].modTime > found[j].modTime
	})

	values := make([]string, 0, len(found))
	seen := map[string]bool{}
	for _, item := range found {
		key := normalizedPathKey(item.path)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		values = append(values, item.path)
	}
	return values
}

func vscodeBundleExtensionsRoot(path string) string {
	path = cleanPath(path)
	if path == "" || !looksLikeVSCodeBundleCodexPath(path) {
		return ""
	}
	extensionDir := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	if !strings.HasPrefix(filepath.Base(extensionDir), "openai.chatgpt-") {
		return ""
	}
	return filepath.Dir(extensionDir)
}

func vscodeBundleExtensionCandidates(extensionDir string) []string {
	binDir := filepath.Join(strings.TrimSpace(extensionDir), "bin")
	dirs, err := os.ReadDir(binDir)
	if err != nil {
		return nil
	}
	values := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		bundleDir := filepath.Join(binDir, dir.Name())
		for _, name := range []string{"codex", "codex.exe", "codex.real", "codex.real.exe"} {
			path := filepath.Join(bundleDir, name)
			if regularFileExists(path) {
				values = append(values, path)
			}
		}
	}
	sort.Strings(values)
	return values
}

func usableVSCodeBundleCodexPath(path string) string {
	path = cleanPath(path)
	if path == "" || !looksLikeVSCodeBundleCodexPath(path) {
		return ""
	}

	switch strings.ToLower(filepath.Base(path)) {
	case "codex.real", "codex.real.exe":
		if regularFileExists(path) {
			return path
		}
		return ""
	case "codex", "codex.exe":
		realPath := editor.ManagedShimRealBinaryPath(path)
		if regularFileExists(realPath) {
			return realPath
		}
		status, err := editor.DetectManagedShim(path, "")
		if err == nil && status.SidecarExists {
			return ""
		}
		if regularFileExists(path) {
			return path
		}
		return ""
	default:
		return ""
	}
}

func looksLikeVSCodeBundleCodexPath(path string) bool {
	normalized := normalizedPathKey(path)
	if normalized == "" {
		return false
	}
	if !strings.Contains(normalized, "/openai.chatgpt-") || !strings.Contains(normalized, "/bin/") {
		return false
	}
	base := filepath.Base(normalized)
	switch base {
	case "codex", "codex.exe", "codex.real", "codex.real.exe":
		return true
	default:
		return false
	}
}

func regularFileExists(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return err == nil && info.Mode().IsRegular()
}

func normalizedPathKey(path string) string {
	path = cleanPath(path)
	if path == "" {
		return ""
	}
	key := strings.ReplaceAll(path, "\\", "/")
	key = strings.TrimPrefix(key, "//?/")
	return strings.ToLower(key)
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}
