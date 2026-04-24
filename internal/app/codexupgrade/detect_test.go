package codexupgrade

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInspectTreatsBundleConfigWithPATHCodexAsStandaloneNPM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses unix shell scripts")
	}

	root := t.TempDir()
	packageRoot := filepath.Join(root, "node", "lib", "node_modules", "@openai", "codex")
	packageBin := filepath.Join(packageRoot, "bin", "codex.js")
	if err := os.MkdirAll(filepath.Dir(packageBin), 0o755); err != nil {
		t.Fatalf("MkdirAll package bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "package.json"), []byte("{\"name\":\"@openai/codex\",\"version\":\"0.124.0\",\"bin\":{\"codex\":\"bin/codex.js\"}}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile package.json: %v", err)
	}
	if err := os.WriteFile(packageBin, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatalf("WriteFile package bin: %v", err)
	}

	prefixBin := filepath.Join(root, "node", "bin")
	if err := os.MkdirAll(prefixBin, 0o755); err != nil {
		t.Fatalf("MkdirAll prefix bin: %v", err)
	}
	if err := os.Symlink(packageBin, filepath.Join(prefixBin, "codex")); err != nil {
		t.Fatalf("Symlink codex: %v", err)
	}

	fakeBin := filepath.Join(root, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("MkdirAll fake bin: %v", err)
	}
	npmScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"config\" ] && [ \"$2\" = \"get\" ] && [ \"$3\" = \"prefix\" ]; then\n" +
		"  printf '%s\\n' \"" + filepath.Join(root, "node") + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"root\" ] && [ \"$2\" = \"-g\" ]; then\n" +
		"  printf '%s\\n' \"" + filepath.Join(root, "node", "lib", "node_modules") + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "npm"), []byte(npmScript), 0o755); err != nil {
		t.Fatalf("WriteFile npm: %v", err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+prefixBin)
	bundlePath := filepath.Join(root, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex.real")
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o755); err != nil {
		t.Fatalf("MkdirAll bundle path: %v", err)
	}
	if err := os.WriteFile(bundlePath, []byte("bundle"), 0o755); err != nil {
		t.Fatalf("WriteFile bundle path: %v", err)
	}

	info := Inspect(context.Background(), InspectOptions{ConfiguredBinary: bundlePath})
	if info.SourceKind != SourceStandaloneNPM {
		t.Fatalf("SourceKind = %q, want %q (%#v)", info.SourceKind, SourceStandaloneNPM, info)
	}
	if info.PackageVersion != "0.124.0" {
		t.Fatalf("PackageVersion = %q", info.PackageVersion)
	}
	if info.EffectiveBinary != "codex" {
		t.Fatalf("EffectiveBinary = %q, want codex", info.EffectiveBinary)
	}
}

func TestInspectTreatsLiveBundleBinaryAsBundleBacked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses unix bundle path")
	}

	root := t.TempDir()
	t.Setenv("PATH", root)

	bundlePath := filepath.Join(root, ".vscode-server", "extensions", "openai.chatgpt-1", "bin", "linux-x86_64", "codex.real")
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o755); err != nil {
		t.Fatalf("MkdirAll bundle path: %v", err)
	}
	if err := os.WriteFile(bundlePath, []byte("bundle"), 0o755); err != nil {
		t.Fatalf("WriteFile bundle path: %v", err)
	}

	info := Inspect(context.Background(), InspectOptions{ConfiguredBinary: bundlePath})
	if info.SourceKind != SourceVSCodeBundle {
		t.Fatalf("SourceKind = %q, want %q", info.SourceKind, SourceVSCodeBundle)
	}
}
