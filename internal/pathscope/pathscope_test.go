package pathscope

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestApplyPrefixNoPrefix(t *testing.T) {
	t.Setenv(EnvFSPrefix, "")
	got := ApplyPrefix(filepath.Join(string(filepath.Separator), "tmp", "demo"))
	if got != filepath.Join(string(filepath.Separator), "tmp", "demo") {
		t.Fatalf("ApplyPrefix() = %q", got)
	}
}

func TestApplyPrefixAbsolutePath(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "sandbox")
	t.Setenv(EnvFSPrefix, prefix)
	target := filepath.Join(string(filepath.Separator), "var", "log", "app.log")
	got := ApplyPrefix(target)
	want := filepath.Join(prefix, "var", "log", "app.log")
	if got != want {
		t.Fatalf("ApplyPrefix() = %q, want %q", got, want)
	}
}

func TestApplyPrefixKeepsPathInsidePrefix(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "sandbox")
	t.Setenv(EnvFSPrefix, prefix)
	target := filepath.Join(prefix, "already", "inside")
	if got := ApplyPrefix(target); got != target {
		t.Fatalf("ApplyPrefix() = %q, want %q", got, target)
	}
}

func TestEnsureWritePathStrictMode(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "sandbox")
	t.Setenv(EnvFSPrefix, prefix)
	t.Setenv(EnvFSStrict, "1")

	if err := EnsureWritePath(filepath.Join(prefix, "config.json")); err != nil {
		t.Fatalf("EnsureWritePath(inside) error = %v", err)
	}

	outside := filepath.Join(string(filepath.Separator), "tmp", "outside.json")
	if runtime.GOOS == "windows" {
		// On windows runners this absolute path is still valid for coverage.
		outside = filepath.Join(string(filepath.Separator), "Windows", "Temp", "outside.json")
	}
	if err := EnsureWritePath(outside); err == nil {
		t.Fatal("EnsureWritePath(outside) expected error")
	}
}

func TestEnsureWritePathStrictDisabled(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "sandbox")
	t.Setenv(EnvFSPrefix, prefix)
	t.Setenv(EnvFSStrict, "0")
	outside := filepath.Join(string(filepath.Separator), "tmp", "outside.json")
	if err := EnsureWritePath(outside); err != nil {
		t.Fatalf("EnsureWritePath(strict=0) error = %v", err)
	}
}
