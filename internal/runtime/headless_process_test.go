package relayruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartDetachedDaemonFailsWithoutBinary(t *testing.T) {
	paths := testPaths(t)
	_, err := StartDetachedDaemon(LaunchOptions{
		BinaryPath: "",
		Paths:      paths,
	})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("StartDetachedDaemon error = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(paths.DaemonLogFile); err != nil {
		t.Fatalf("expected daemon log file to be created before failure, stat err=%v", err)
	}
}

func TestStartDetachedWrapperFailsForMissingBinary(t *testing.T) {
	paths := testPaths(t)
	_, err := StartDetachedWrapper(HeadlessLaunchOptions{
		BinaryPath: filepath.Join(t.TempDir(), "missing-wrapper"),
		Paths:      paths,
		InstanceID: "chat/abc:1",
	})
	if err == nil {
		t.Fatal("expected StartDetachedWrapper to fail for missing binary")
	}
	logPath := filepath.Join(paths.LogsDir, "codex-remote-headless-chat_abc_1.log")
	if _, statErr := os.Stat(logPath); statErr != nil {
		t.Fatalf("expected sanitized wrapper log file, stat err=%v", statErr)
	}
}

func TestSanitizeFilenameAndTerminateProcessWrapper(t *testing.T) {
	if got := sanitizeFilename(""); got != "unknown" {
		t.Fatalf("sanitizeFilename empty = %q", got)
	}
	if got := sanitizeFilename("Abc-123_.:/\\x"); got != "Abc-123_.___x" {
		t.Fatalf("sanitizeFilename = %q", got)
	}
	if err := TerminateProcess(0, 10*time.Millisecond); err != nil {
		t.Fatalf("TerminateProcess: %v", err)
	}
}
