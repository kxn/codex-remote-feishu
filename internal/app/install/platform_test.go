package install

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultVSCodeSettingsPathWindowsUsesAppData(t *testing.T) {
	t.Setenv("APPDATA", filepath.Join("C:\\", "Users", "demo", "AppData", "Roaming"))
	got := defaultVSCodeSettingsPath("windows", filepath.Join("C:\\", "Users", "demo"))
	want := filepath.Join("C:\\", "Users", "demo", "AppData", "Roaming", "Code", "User", "settings.json")
	if got != want {
		t.Fatalf("defaultVSCodeSettingsPath()=%q, want %q", got, want)
	}
}

func TestDefaultInstallBinDirForDefaultInstanceUsesNamespacedDataDir(t *testing.T) {
	linuxHome := filepath.Join(string(filepath.Separator), "home", "demo")
	if got, want := defaultInstallBinDirForInstance("linux", linuxHome, defaultInstanceID), filepath.Join(linuxHome, ".local", "share", "codex-remote", "bin"); got != want {
		t.Fatalf("linux defaultInstallBinDirForInstance()=%q, want %q", got, want)
	}

	darwinHome := filepath.Join(string(filepath.Separator), "Users", "demo")
	if got, want := defaultInstallBinDirForInstance("darwin", darwinHome, defaultInstanceID), filepath.Join(darwinHome, "Library", "Application Support", "codex-remote", "bin"); got != want {
		t.Fatalf("darwin defaultInstallBinDirForInstance()=%q, want %q", got, want)
	}
}

func TestDetectBundleEntrypointsIgnoresRealBinary(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".vscode-server", "extensions")
	older := filepath.Join(root, "openai.chatgpt-1.0.0")
	newer := filepath.Join(root, "openai.chatgpt-2.0.0")

	writeExecutable(t, filepath.Join(older, "bin", "linux-x86_64", "codex"), "old")
	writeExecutable(t, filepath.Join(older, "bin", "linux-x86_64", "codex.real"), "old-real")
	writeExecutable(t, filepath.Join(newer, "bin", "linux-x86_64", "codex"), "new")
	writeExecutable(t, filepath.Join(newer, "bin", "linux-x86_64", "codex.real"), "new-real")

	now := time.Now()
	if err := os.Chtimes(older, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("chtimes older: %v", err)
	}
	if err := os.Chtimes(newer, now, now); err != nil {
		t.Fatalf("chtimes newer: %v", err)
	}

	got := detectBundleEntrypoints("linux", "amd64", home)
	want := []string{
		filepath.Join(newer, "bin", "linux-x86_64", "codex"),
		filepath.Join(older, "bin", "linux-x86_64", "codex"),
	}
	if len(got) != len(want) {
		t.Fatalf("detectBundleEntrypoints len=%d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("detectBundleEntrypoints[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestDetectBundleEntrypointsWindowsSkipsOtherPlatforms(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".vscode", "extensions")
	older := filepath.Join(root, "openai.chatgpt-1.0.0")
	newer := filepath.Join(root, "openai.chatgpt-2.0.0")

	writeExecutable(t, filepath.Join(older, "bin", "linux-x86_64", "codex"), "old-linux")
	writeExecutable(t, filepath.Join(older, "bin", "windows-x86_64", "codex.exe"), "old-win")
	writeExecutable(t, filepath.Join(newer, "bin", "linux-x86_64", "codex"), "new-linux")
	writeExecutable(t, filepath.Join(newer, "bin", "windows-x86_64", "codex.exe"), "new-win")

	now := time.Now()
	if err := os.Chtimes(older, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("chtimes older: %v", err)
	}
	if err := os.Chtimes(newer, now, now); err != nil {
		t.Fatalf("chtimes newer: %v", err)
	}

	got := detectBundleEntrypoints("windows", "amd64", home)
	want := []string{
		filepath.Join(newer, "bin", "windows-x86_64", "codex.exe"),
		filepath.Join(older, "bin", "windows-x86_64", "codex.exe"),
	}
	assertEntrypointsEqual(t, got, want)
}

func TestDetectBundleEntrypointsLinuxSkipsOtherPlatforms(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".vscode-server", "extensions")
	older := filepath.Join(root, "openai.chatgpt-1.0.0")
	newer := filepath.Join(root, "openai.chatgpt-2.0.0")

	writeExecutable(t, filepath.Join(older, "bin", "darwin-arm64", "codex"), "old-darwin")
	writeExecutable(t, filepath.Join(older, "bin", "linux-x86_64", "codex"), "old-linux")
	writeExecutable(t, filepath.Join(newer, "bin", "windows-x86_64", "codex.exe"), "new-win")
	writeExecutable(t, filepath.Join(newer, "bin", "linux-x86_64", "codex"), "new-linux")

	now := time.Now()
	if err := os.Chtimes(older, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("chtimes older: %v", err)
	}
	if err := os.Chtimes(newer, now, now); err != nil {
		t.Fatalf("chtimes newer: %v", err)
	}

	got := detectBundleEntrypoints("linux", "amd64", home)
	want := []string{
		filepath.Join(newer, "bin", "linux-x86_64", "codex"),
		filepath.Join(older, "bin", "linux-x86_64", "codex"),
	}
	assertEntrypointsEqual(t, got, want)
}

func TestDetectBundleEntrypointsPrefersCurrentArchOnDarwin(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".vscode", "extensions")
	extensionDir := filepath.Join(root, "openai.chatgpt-1.0.0")

	writeExecutable(t, filepath.Join(extensionDir, "bin", "darwin-universal", "codex"), "universal")
	writeExecutable(t, filepath.Join(extensionDir, "bin", "darwin-x86_64", "codex"), "intel")
	writeExecutable(t, filepath.Join(extensionDir, "bin", "darwin-arm64", "codex"), "apple-silicon")

	got := detectBundleEntrypoints("darwin", "arm64", home)
	want := []string{
		filepath.Join(extensionDir, "bin", "darwin-arm64", "codex"),
	}
	assertEntrypointsEqual(t, got, want)
}

func assertEntrypointsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("detectBundleEntrypoints len=%d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("detectBundleEntrypoints[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
