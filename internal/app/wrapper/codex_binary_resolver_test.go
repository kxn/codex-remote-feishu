package wrapper

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestResolveNormalCodexBinaryHealsVSCodePathToPATHCodex(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	pathDir := filepath.Join(home, "bin")
	writeResolverExecutable(t, filepath.Join(pathDir, "codex"))
	t.Setenv("PATH", pathDir)

	configPath := writeResolverConfig(t, home, filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex.real"))

	got, err := resolveNormalCodexBinary(configPath, filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex.real"))
	if err != nil {
		t.Fatalf("resolveNormalCodexBinary: %v", err)
	}
	if got != "codex" {
		t.Fatalf("resolved codex binary = %q, want codex", got)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Wrapper.CodexRealBinary != "codex" {
		t.Fatalf("config codex real binary = %q, want codex", loaded.Config.Wrapper.CodexRealBinary)
	}
}

func TestResolveNormalCodexBinaryFallsBackToUsableVSCodeBundle(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("PATH", filepath.Join(home, "empty-bin"))
	vscodeRoot := filepath.Join(home, ".vscode-server", "extensions")
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", vscodeRoot)

	entrypoint := filepath.Join(vscodeRoot, "openai.chatgpt-2", "bin", "linux-x86_64", "codex")
	realPath := entrypoint + ".real"
	writeResolverExecutable(t, entrypoint)
	writeResolverExecutable(t, realPath)

	stale := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex.real")
	configPath := writeResolverConfig(t, home, stale)

	got, err := resolveNormalCodexBinary(configPath, stale)
	if err != nil {
		t.Fatalf("resolveNormalCodexBinary: %v", err)
	}
	if got != realPath {
		t.Fatalf("resolved codex binary = %q, want %q", got, realPath)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Wrapper.CodexRealBinary != "codex" {
		t.Fatalf("config codex real binary = %q, want codex", loaded.Config.Wrapper.CodexRealBinary)
	}
}

func TestResolveNormalCodexBinaryFallsBackToSiblingBundleAcrossPlatformDirs(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("PATH", filepath.Join(home, "empty-bin"))

	vscodeRoot := filepath.Join(home, ".vscode-server", "extensions")
	entrypoint := filepath.Join(vscodeRoot, "openai.chatgpt-2", "bin", "windows-x64", "codex.exe")
	realPath := filepath.Join(vscodeRoot, "openai.chatgpt-2", "bin", "windows-x64", "codex.real.exe")
	writeResolverExecutable(t, entrypoint)
	writeResolverExecutable(t, realPath)

	stale := filepath.Join(vscodeRoot, "openai.chatgpt-1", "bin", "linux-x86_64", "codex.real")
	configPath := writeResolverConfig(t, home, stale)

	got, err := resolveNormalCodexBinary(configPath, stale)
	if err != nil {
		t.Fatalf("resolveNormalCodexBinary: %v", err)
	}
	if got != realPath {
		t.Fatalf("resolved codex binary = %q, want %q", got, realPath)
	}
}

func TestResolveNormalCodexBinaryErrorsWithoutPATHOrVSCodeFallback(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("PATH", filepath.Join(home, "empty-bin"))

	stale := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex.real")
	configPath := writeResolverConfig(t, home, stale)

	if _, err := resolveNormalCodexBinary(configPath, stale); err == nil {
		t.Fatal("expected resolveNormalCodexBinary to fail")
	}
}

func TestResolveNormalCodexBinaryFallsBackWhenConfiguredPATHCodexMissing(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("PATH", filepath.Join(home, "empty-bin"))

	vscodeRoot := filepath.Join(home, ".vscode-server", "extensions")
	t.Setenv("VSCODE_SERVER_EXTENSIONS_DIR", vscodeRoot)
	entrypoint, realPath := writeResolverRuntimeBundle(t, vscodeRoot, "openai.chatgpt-2")
	writeResolverExecutable(t, entrypoint)
	writeResolverExecutable(t, realPath)

	configPath := writeResolverConfig(t, home, "codex")

	got, err := resolveNormalCodexBinary(configPath, "codex")
	if err != nil {
		t.Fatalf("resolveNormalCodexBinary: %v", err)
	}
	if got != realPath {
		t.Fatalf("resolved codex binary = %q, want %q", got, realPath)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Wrapper.CodexRealBinary != "codex" {
		t.Fatalf("config codex real binary = %q, want codex", loaded.Config.Wrapper.CodexRealBinary)
	}
}

func TestResolveNormalCodexBinaryErrorsWhenConfiguredPATHCodexMissingWithoutFallback(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("PATH", filepath.Join(home, "empty-bin"))

	configPath := writeResolverConfig(t, home, "codex")

	if _, err := resolveNormalCodexBinary(configPath, "codex"); err == nil {
		t.Fatal("expected resolveNormalCodexBinary to fail")
	}
}

func TestResolveNormalCodexBinarySkipsHealingForExplicitOverride(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("CODEX_REAL_BINARY", filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex.real"))

	configured := filepath.Join(home, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex.real")
	configPath := writeResolverConfig(t, home, configured)

	got, err := resolveNormalCodexBinary(configPath, configured)
	if err != nil {
		t.Fatalf("resolveNormalCodexBinary: %v", err)
	}
	if got != configured {
		t.Fatalf("resolved codex binary = %q, want %q", got, configured)
	}
}

func writeResolverConfig(t *testing.T, home, codexRealBinary string) string {
	t.Helper()
	configPath := filepath.Join(home, ".config", "codex-remote", "config.json")
	cfg := config.DefaultAppConfig()
	cfg.Wrapper.CodexRealBinary = codexRealBinary
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	return configPath
}

func writeResolverExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte("resolver-bin"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		if err := os.WriteFile(path+".exe", []byte("resolver-bin"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q): %v", path+".exe", err)
		}
	}
}

func writeResolverRuntimeBundle(t *testing.T, vscodeRoot, version string) (string, string) {
	t.Helper()
	bundleDir := filepath.Join(vscodeRoot, version, "bin", resolverRuntimeBundleDir())
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(bundleDir, "codex.exe"), filepath.Join(bundleDir, "codex.real.exe")
	default:
		return filepath.Join(bundleDir, "codex"), filepath.Join(bundleDir, "codex.real")
	}
}

func resolverRuntimeBundleDir() string {
	switch runtime.GOOS {
	case "windows":
		return resolverRuntimeBundleDirForArch("windows", runtime.GOARCH)
	case "darwin":
		return resolverRuntimeBundleDirForArch("darwin", runtime.GOARCH)
	default:
		return resolverRuntimeBundleDirForArch("linux", runtime.GOARCH)
	}
}

func resolverRuntimeBundleDirForArch(goos, goarch string) string {
	switch goos {
	case "windows":
		switch goarch {
		case "386":
			return "windows-x86"
		case "arm64":
			return "windows-arm64"
		default:
			return "windows-x64"
		}
	case "darwin":
		switch goarch {
		case "amd64":
			return "darwin-x86_64"
		default:
			return "darwin-arm64"
		}
	default:
		switch goarch {
		case "386":
			return "linux-x86"
		case "arm64":
			return "linux-arm64"
		default:
			return "linux-x86_64"
		}
	}
}

// setTestHome points both HOME and USERPROFILE at the throwaway dir. Windows
// os.UserHomeDir() reads USERPROFILE, not HOME, so tests that only set HOME
// would silently resolve the real user's VS Code extension bundles and patch
// or read the real machine's codex installation.
func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}
