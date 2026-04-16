package wrapper

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
