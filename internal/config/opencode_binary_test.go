package config

import (
	"fmt"
	"path/filepath"
	"testing"
)

func withOpenCodeShellLookupForTest(t *testing.T, fn func(env []string, key string) (string, error)) {
	t.Helper()
	original := lookupUserShellEnvValue
	lookupUserShellEnvValue = fn
	t.Cleanup(func() {
		lookupUserShellEnvValue = original
	})
}

func TestResolveOpenCodeBinaryUsesExplicitEnvPath(t *testing.T) {
	withOpenCodeShellLookupForTest(t, func(env []string, key string) (string, error) {
		return "", fmt.Errorf("shell unavailable")
	})
	opencodePath := filepath.Join(t.TempDir(), executableNameForTest("opencode-env"))
	writeExecutableFileForTest(t, opencodePath)

	got, err := ResolveOpenCodeBinary([]string{
		"HOME=" + t.TempDir(),
		OpenCodeBinaryEnv + "=" + opencodePath,
		"PATH=/usr/bin",
	}, "")
	if err != nil {
		t.Fatalf("ResolveOpenCodeBinary: %v", err)
	}
	assertResolvedExecutablePath(t, got, opencodePath)
}

func TestResolveOpenCodeBinaryUsesConfigPath(t *testing.T) {
	withOpenCodeShellLookupForTest(t, func(env []string, key string) (string, error) {
		return "", fmt.Errorf("shell unavailable")
	})
	opencodePath := filepath.Join(t.TempDir(), executableNameForTest("opencode-config"))
	writeExecutableFileForTest(t, opencodePath)

	got, err := ResolveOpenCodeBinary([]string{"HOME=" + t.TempDir(), "PATH=/usr/bin"}, opencodePath)
	if err != nil {
		t.Fatalf("ResolveOpenCodeBinary: %v", err)
	}
	assertResolvedExecutablePath(t, got, opencodePath)
}

func TestResolveOpenCodeBinaryFallsBackToPATH(t *testing.T) {
	withOpenCodeShellLookupForTest(t, func(env []string, key string) (string, error) {
		if key != "PATH" {
			t.Fatalf("lookup key = %q, want PATH", key)
		}
		return "/missing", nil
	})
	pathDir := t.TempDir()
	opencodePath := filepath.Join(pathDir, executableNameForTest("opencode"))
	writeExecutableFileForTest(t, opencodePath)

	got, err := ResolveOpenCodeBinary([]string{"HOME=" + t.TempDir(), "PATH=" + pathDir}, "")
	if err != nil {
		t.Fatalf("ResolveOpenCodeBinary: %v", err)
	}
	assertResolvedExecutablePath(t, got, opencodePath)
}
