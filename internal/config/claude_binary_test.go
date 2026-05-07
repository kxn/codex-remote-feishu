package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveClaudeBinaryUsesExplicitAbsolutePath(t *testing.T) {
	originalLookup := lookupUserShellEnvValue
	defer func() { lookupUserShellEnvValue = originalLookup }()
	lookupUserShellEnvValue = func(env []string, key string) (string, error) {
		return "", fmt.Errorf("shell unavailable")
	}

	claudePath := filepath.Join(t.TempDir(), executableNameForTest("claude-explicit"))
	writeExecutableFileForTest(t, claudePath)

	got, err := ResolveClaudeBinary([]string{
		"HOME=" + t.TempDir(),
		ClaudeBinaryEnv + "=" + claudePath,
		"PATH=/usr/bin",
	})
	if err != nil {
		t.Fatalf("ResolveClaudeBinary: %v", err)
	}
	assertResolvedExecutablePath(t, got, claudePath)
}

func assertResolvedExecutablePath(t *testing.T, got, want string) {
	t.Helper()
	got = normalizeExecutablePathForTest(t, got)
	want = normalizeExecutablePathForTest(t, want)
	if got != want {
		t.Fatalf("resolved executable path = %q, want %q", got, want)
	}
}

func normalizeExecutablePathForTest(t *testing.T, path string) string {
	t.Helper()
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && strings.TrimSpace(resolved) != "" {
		path = resolved
	}
	return filepath.Clean(path)
}

func TestResolveClaudeBinaryUsesExplicitCommandFromShellPATH(t *testing.T) {
	originalLookup := lookupUserShellEnvValue
	defer func() { lookupUserShellEnvValue = originalLookup }()

	shellDir := t.TempDir()
	shellBinary := filepath.Join(shellDir, executableNameForTest("claude-alt"))
	writeExecutableFileForTest(t, shellBinary)
	lookupUserShellEnvValue = func(env []string, key string) (string, error) {
		if key != "PATH" {
			t.Fatalf("lookup key = %q, want PATH", key)
		}
		return shellDir, nil
	}

	got, err := ResolveClaudeBinary([]string{
		"HOME=" + t.TempDir(),
		ClaudeBinaryEnv + "=claude-alt",
		"PATH=/usr/bin",
	})
	if err != nil {
		t.Fatalf("ResolveClaudeBinary: %v", err)
	}
	assertResolvedExecutablePath(t, got, shellBinary)
}

func TestResolveClaudeBinaryPrefersShellPATHBeforeCurrentPATH(t *testing.T) {
	originalLookup := lookupUserShellEnvValue
	defer func() { lookupUserShellEnvValue = originalLookup }()

	currentDir := t.TempDir()
	currentBinary := filepath.Join(currentDir, executableNameForTest("claude"))
	writeExecutableFileForTest(t, currentBinary)
	shellDir := t.TempDir()
	shellBinary := filepath.Join(shellDir, executableNameForTest("claude"))
	writeExecutableFileForTest(t, shellBinary)
	lookupUserShellEnvValue = func(env []string, key string) (string, error) {
		if key != "PATH" {
			t.Fatalf("lookup key = %q, want PATH", key)
		}
		return shellDir, nil
	}

	got, err := ResolveClaudeBinary([]string{
		"HOME=" + t.TempDir(),
		"PATH=" + currentDir,
	})
	if err != nil {
		t.Fatalf("ResolveClaudeBinary: %v", err)
	}
	assertResolvedExecutablePath(t, got, shellBinary)
}

func TestResolveClaudeBinaryFallsBackToCurrentPATH(t *testing.T) {
	originalLookup := lookupUserShellEnvValue
	defer func() { lookupUserShellEnvValue = originalLookup }()
	lookupUserShellEnvValue = func(env []string, key string) (string, error) {
		return "", fmt.Errorf("shell unavailable")
	}

	currentDir := t.TempDir()
	currentBinary := filepath.Join(currentDir, executableNameForTest("claude"))
	writeExecutableFileForTest(t, currentBinary)

	got, err := ResolveClaudeBinary([]string{
		"HOME=" + t.TempDir(),
		"PATH=" + currentDir,
	})
	if err != nil {
		t.Fatalf("ResolveClaudeBinary: %v", err)
	}
	assertResolvedExecutablePath(t, got, currentBinary)
}

func TestResolveClaudeBinaryReturnsErrorWhenUnavailable(t *testing.T) {
	originalLookup := lookupUserShellEnvValue
	defer func() { lookupUserShellEnvValue = originalLookup }()
	lookupUserShellEnvValue = func(env []string, key string) (string, error) {
		return "", fmt.Errorf("shell unavailable")
	}

	if _, err := ResolveClaudeBinary([]string{
		"HOME=" + t.TempDir(),
		"PATH=" + t.TempDir(),
	}); err == nil {
		t.Fatal("expected ResolveClaudeBinary to fail when claude is unavailable")
	}
}

func writeExecutableFileForTest(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	content := []byte("#!/bin/sh\nexit 0\n")
	if executableNameForTest("x") != "x" {
		content = []byte("echo off\r\n")
	}
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func executableNameForTest(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}
