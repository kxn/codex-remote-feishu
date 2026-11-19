package wrapper

import (
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestDetectLocalShellAndFixedReadCommands(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		goos string
		kind localShellKind
		path string
		want string
	}{
		{name: "posix", env: []string{"SHELL=/bin/zsh"}, goos: "linux", kind: localShellPOSIX, path: "/bin/zsh", want: "cat '/tmp/payload'"},
		{name: "powershell", env: []string{"CODEX_REMOTE_SHELL=pwsh.exe"}, goos: "windows", kind: localShellPowerShell, path: "pwsh.exe", want: "Get-Content -Raw -Encoding UTF8 -LiteralPath '/tmp/payload'"},
		{name: "cmd", env: []string{"ComSpec=C:\\Windows\\System32\\cmd.exe"}, goos: "windows", kind: localShellCMD, path: "C:\\Windows\\System32\\cmd.exe", want: `type "/tmp/payload"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, path, ok := detectLocalShell(tt.env, tt.goos)
			if !ok || kind != tt.kind || path != tt.path {
				t.Fatalf("detectLocalShell = %q, %q, %t", kind, path, ok)
			}
			got, err := fixedShellReadCommand(kind, "/tmp/payload")
			if err != nil || got != tt.want {
				t.Fatalf("fixedShellReadCommand = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestShellCommandRuntimeWritesProtectedPayloadAndCleansIt(t *testing.T) {
	stateDir := t.TempDir()
	runtime := newShellCommandRuntime(stateDir, "instance-1")
	command, cleanup, err := runtime.prepare(agentproto.Command{
		Kind:   agentproto.CommandThreadShellCommand,
		Target: agentproto.Target{ThreadID: "thread-1", TurnID: "turn-1"},
		ShellCommand: agentproto.ShellCommand{
			Payload: "<queued_input_bundle_v1>\n{\"text\":\"$()\"}\n</queued_input_bundle_v1>\n",
		},
	})
	if err != nil {
		t.Fatalf("prepare shell command: %v", err)
	}
	if strings.Contains(command.ShellCommand.Command, "$()") || !strings.Contains(command.ShellCommand.Command, "payload-") {
		t.Fatalf("user payload leaked into shell command: %q", command.ShellCommand.Command)
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "applause-shell", "runtime-instance-1"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("payload directory entries = %#v, err=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatalf("payload info: %v", err)
	}
	if goruntime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("payload mode = %v, want 0600", info.Mode().Perm())
	}
	cleanup(true)
	entries, err = os.ReadDir(filepath.Join(stateDir, "applause-shell", "runtime-instance-1"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("accepted shell command payload was cleaned too early: %#v, err=%v", entries, err)
	}
	runtime.observeEvents([]agentproto.Event{
		{
			Kind:     agentproto.EventItemStarted,
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "item-1",
			ItemKind: "command_execution",
			Metadata: map[string]any{"source": "userShell"},
		},
		{
			Kind:     agentproto.EventItemCompleted,
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "item-1",
			ItemKind: "command_execution",
			Metadata: map[string]any{"source": "userShell"},
		},
	})
	entries, err = os.ReadDir(filepath.Join(stateDir, "applause-shell", "runtime-instance-1"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("payload was not cleaned after UserShell completion: %#v, err=%v", entries, err)
	}

	command, cleanup, err = runtime.prepare(agentproto.Command{
		Kind:   agentproto.CommandThreadShellCommand,
		Target: agentproto.Target{ThreadID: "thread-1", TurnID: "turn-1"},
		ShellCommand: agentproto.ShellCommand{
			Payload: "rejected",
		},
	})
	if err != nil {
		t.Fatalf("prepare rejected shell command: %v", err)
	}
	_ = command
	cleanup(false)
	entries, err = os.ReadDir(filepath.Join(stateDir, "applause-shell", "runtime-instance-1"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected payload was not cleaned: %#v, err=%v", entries, err)
	}

	_, cleanup, err = runtime.prepare(agentproto.Command{
		Kind:   agentproto.CommandThreadShellCommand,
		Target: agentproto.Target{ThreadID: "thread-1", TurnID: "turn-1"},
		ShellCommand: agentproto.ShellCommand{
			Payload: "runtime cleanup",
		},
	})
	if err != nil {
		t.Fatalf("prepare runtime-cleanup shell command: %v", err)
	}
	cleanup(true)
	runtime.cleanupCurrentRuntime()
	if _, err := os.Stat(filepath.Join(stateDir, "applause-shell", "runtime-instance-1")); !os.IsNotExist(err) {
		t.Fatalf("current runtime directory was not cleaned on shutdown: %v", err)
	}
}

func TestShellCommandRuntimeCachesDetectionAndNegativeResults(t *testing.T) {
	runtime := &shellCommandRuntime{
		detectionKind: localShellPOSIX,
		detectionPath: "/bin/sh",
		detected:      true,
		payloads:      map[string][]*shellCommandPayload{},
	}
	if kind, path, err := runtime.detect(); err != nil || kind != localShellPOSIX || path != "/bin/sh" {
		t.Fatalf("cached detection = %q, %q, %v", kind, path, err)
	}

	negative := errors.New("no shell")
	runtime = &shellCommandRuntime{
		detectionError: negative,
		negativeUntil:  time.Now().Add(time.Minute),
		payloads:       map[string][]*shellCommandPayload{},
	}
	if kind, path, err := runtime.detect(); kind != "" || path != "" || !errors.Is(err, negative) {
		t.Fatalf("cached negative detection = %q, %q, %v", kind, path, err)
	}
}

func TestCleanupExpiredShellCommandPayloadsOnlyRemovesOldRuntimeDirectories(t *testing.T) {
	stateDir := t.TempDir()
	root := filepath.Join(stateDir, "applause-shell")
	oldRuntime := filepath.Join(root, "runtime-old")
	recentRuntime := filepath.Join(root, "runtime-recent")
	otherDir := filepath.Join(root, "other")
	for _, path := range []string{oldRuntime, recentRuntime, otherDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	now := time.Now()
	old := now.Add(-shellCommandPayloadTTL - time.Minute)
	if err := os.Chtimes(oldRuntime, old, old); err != nil {
		t.Fatalf("age old runtime: %v", err)
	}
	if err := cleanupExpiredShellCommandPayloads(stateDir, now); err != nil {
		t.Fatalf("cleanup expired payloads: %v", err)
	}
	if _, err := os.Stat(oldRuntime); !os.IsNotExist(err) {
		t.Fatalf("old runtime was not removed: %v", err)
	}
	if _, err := os.Stat(recentRuntime); err != nil {
		t.Fatalf("recent runtime was removed: %v", err)
	}
	if _, err := os.Stat(otherDir); err != nil {
		t.Fatalf("unrelated directory was removed: %v", err)
	}
}

func TestShellCommandRuntimeRejectsSymlinkedPayloadRoot(t *testing.T) {
	stateDir := t.TempDir()
	targetDir := t.TempDir()
	root := filepath.Join(stateDir, "applause-shell")
	if err := os.Symlink(targetDir, root); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	runtime := newShellCommandRuntime(stateDir, "instance-1")
	_, _, err := runtime.prepare(agentproto.Command{
		Kind:   agentproto.CommandThreadShellCommand,
		Target: agentproto.Target{ThreadID: "thread-1", TurnID: "turn-1"},
		ShellCommand: agentproto.ShellCommand{
			Payload: "must not follow symlink",
		},
	})
	if err == nil {
		t.Fatal("expected symlinked payload root to fail closed")
	}
	entries, readErr := os.ReadDir(targetDir)
	if readErr != nil {
		t.Fatalf("read symlink target: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("payload was written through symlink: %#v", entries)
	}
}
