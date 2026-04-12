package relayruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestBinaryIdentityHelpersAndPersistence(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "codex.real")
	if err := os.WriteFile(binaryPath, []byte("binary payload"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	identity, err := BinaryIdentityForPathWithBranch(binaryPath, "1.2.3", "release/1.5")
	if err != nil {
		t.Fatalf("BinaryIdentityForPath: %v", err)
	}
	if identity.Product != ProductName {
		t.Fatalf("product = %q, want %q", identity.Product, ProductName)
	}
	if identity.Version != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", identity.Version)
	}
	if identity.Branch != "release/1.5" {
		t.Fatalf("branch = %q, want release/1.5", identity.Branch)
	}
	if !strings.HasPrefix(identity.BuildFingerprint, "sha256:") {
		t.Fatalf("fingerprint = %q, want sha256 prefix", identity.BuildFingerprint)
	}
	wantPath := binaryPath
	if real, err := filepath.EvalSymlinks(binaryPath); err == nil {
		wantPath = real
	}
	if identity.BinaryPath != wantPath {
		t.Fatalf("binary path = %q, want %q", identity.BinaryPath, wantPath)
	}

	startedAt := time.Unix(1_700_000_000, 0).UTC()
	serverIdentity, err := NewServerIdentityWithBranch("9.9.9", "master", filepath.Join(dir, "config.json"), startedAt)
	if err != nil {
		t.Fatalf("NewServerIdentity: %v", err)
	}
	if serverIdentity.Product != ProductName {
		t.Fatalf("server product = %q, want %q", serverIdentity.Product, ProductName)
	}
	if serverIdentity.ConfigPath == "" || serverIdentity.PID != os.Getpid() {
		t.Fatalf("unexpected server identity: %#v", serverIdentity)
	}
	if serverIdentity.Branch != "master" {
		t.Fatalf("server branch = %q, want master", serverIdentity.Branch)
	}
	if !serverIdentity.StartedAt.Equal(startedAt) {
		t.Fatalf("startedAt = %v, want %v", serverIdentity.StartedAt, startedAt)
	}

	identityPath := filepath.Join(dir, "state", "identity.json")
	if err := WriteServerIdentity(identityPath, serverIdentity); err != nil {
		t.Fatalf("WriteServerIdentity: %v", err)
	}
	readIdentity, err := ReadServerIdentity(identityPath)
	if err != nil {
		t.Fatalf("ReadServerIdentity: %v", err)
	}
	if readIdentity.PID != serverIdentity.PID || readIdentity.BuildFingerprint != serverIdentity.BuildFingerprint {
		t.Fatalf("read identity = %#v, want %#v", readIdentity, serverIdentity)
	}

	pidPath := filepath.Join(dir, "state", "daemon.pid")
	if err := WritePID(pidPath, 4321); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
	pid, err := ReadPID(pidPath)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}
	if pid != 4321 {
		t.Fatalf("pid = %d, want 4321", pid)
	}
	if err := os.WriteFile(pidPath, []byte("not-a-pid"), 0o644); err != nil {
		t.Fatalf("write invalid pid: %v", err)
	}
	if _, err := ReadPID(pidPath); err == nil {
		t.Fatal("expected invalid pid parse error")
	}
}

func TestCompatibleIdentityFallbacks(t *testing.T) {
	if !CompatibleIdentity(
		testBinaryIdentity(),
		testBinaryIdentity(),
	) {
		t.Fatal("expected identical fingerprints to be compatible")
	}
	if !CompatibleIdentity(
		testBinaryIdentity(),
		agentprotoIdentity("", "1.0.0", ""),
	) {
		t.Fatal("expected matching versions to be compatible when fingerprints are absent")
	}
	if CompatibleIdentity(
		testBinaryIdentity(),
		agentprotoIdentity("other-product", "1.0.0", "fp-1"),
	) {
		t.Fatal("expected different product to be incompatible")
	}
	if CompatibleIdentity(
		agentprotoIdentity(ProductName, "", ""),
		agentprotoIdentity(ProductName, "", ""),
	) {
		t.Fatal("expected missing version and fingerprint to be incompatible")
	}
}

func TestDefaultPathsAndHelpers(t *testing.T) {
	t.Run("xdg env", func(t *testing.T) {
		configHome := filepath.Join(t.TempDir(), "config")
		dataHome := filepath.Join(t.TempDir(), "data")
		stateHome := filepath.Join(t.TempDir(), "state")
		t.Setenv("XDG_CONFIG_HOME", configHome)
		t.Setenv("XDG_DATA_HOME", dataHome)
		t.Setenv("XDG_STATE_HOME", stateHome)

		paths, err := DefaultPaths()
		if err != nil {
			t.Fatalf("DefaultPaths: %v", err)
		}
		if paths.ConfigFile != filepath.Join(configHome, ProductName, "config.json") {
			t.Fatalf("unexpected config file: %q", paths.ConfigFile)
		}
		if paths.DaemonLogFile != filepath.Join(dataHome, ProductName, "logs", "codex-remote-relayd.log") {
			t.Fatalf("unexpected daemon log: %q", paths.DaemonLogFile)
		}
		if paths.ManagerLockFile != filepath.Join(stateHome, ProductName, "relay-manager.lock") {
			t.Fatalf("unexpected manager lock: %q", paths.ManagerLockFile)
		}
	})

	t.Run("home fallback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("XDG_DATA_HOME", "")
		t.Setenv("XDG_STATE_HOME", "")

		base, err := xdgBase("XDG_CONFIG_HOME", ".config")
		if err != nil {
			t.Fatalf("xdgBase: %v", err)
		}
		if base != filepath.Join(home, ".config") {
			t.Fatalf("xdgBase = %q, want %q", base, filepath.Join(home, ".config"))
		}
	})

	if got := WrapperRawLogFile("/tmp/logs", 123); got != "/tmp/logs/codex-remote-wrapper-123-raw.ndjson" {
		t.Fatalf("WrapperRawLogFile = %q", got)
	}
	if got := WrapperRawLogFile("/tmp/logs", 0); got != "/tmp/logs/codex-remote-wrapper-unknown-raw.ndjson" {
		t.Fatalf("WrapperRawLogFile unknown = %q", got)
	}
}

func agentprotoIdentity(product, version, fingerprint string) agentproto.BinaryIdentity {
	return agentproto.BinaryIdentity{
		Product:          product,
		Version:          version,
		BuildFingerprint: fingerprint,
	}
}
