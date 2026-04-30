package claude

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudePermissionSelectionFromOverrides(t *testing.T) {
	t.Run("full access maps to bypass permissions", func(t *testing.T) {
		selection := claudePermissionSelectionFromOverrides(agentproto.AccessModeFullAccess, "off")
		if selection.NativeMode != claudePermissionModeBypassPermissions {
			t.Fatalf("expected bypassPermissions, got %#v", selection)
		}
		if selection.AccessMode != agentproto.AccessModeFullAccess || selection.PlanMode != "off" {
			t.Fatalf("unexpected selection: %#v", selection)
		}
	})

	t.Run("confirm maps to default", func(t *testing.T) {
		selection := claudePermissionSelectionFromOverrides(agentproto.AccessModeConfirm, "off")
		if selection.NativeMode != claudePermissionModeDefault {
			t.Fatalf("expected default, got %#v", selection)
		}
		if selection.AccessMode != agentproto.AccessModeConfirm || selection.PlanMode != "off" {
			t.Fatalf("unexpected selection: %#v", selection)
		}
	})

	t.Run("plan overrides access mode natively", func(t *testing.T) {
		selection := claudePermissionSelectionFromOverrides(agentproto.AccessModeFullAccess, "on")
		if selection.NativeMode != claudePermissionModePlan {
			t.Fatalf("expected plan, got %#v", selection)
		}
		if selection.AccessMode != "" || selection.PlanMode != "on" {
			t.Fatalf("unexpected plan selection: %#v", selection)
		}
	})

	t.Run("empty access stays default", func(t *testing.T) {
		selection := claudePermissionSelectionFromOverrides("", "")
		if selection.NativeMode != claudePermissionModeDefault {
			t.Fatalf("expected default for empty overrides, got %#v", selection)
		}
	})
}

func TestClaudePermissionSelectionFromNative(t *testing.T) {
	cases := []struct {
		name       string
		nativeMode string
		accessMode string
		planMode   string
	}{
		{name: "default", nativeMode: "default", accessMode: agentproto.AccessModeConfirm, planMode: "off"},
		{name: "bypass", nativeMode: "bypassPermissions", accessMode: agentproto.AccessModeFullAccess, planMode: "off"},
		{name: "plan", nativeMode: "plan", accessMode: "", planMode: "on"},
		{name: "unknown defaults to confirm", nativeMode: "dontAsk", accessMode: agentproto.AccessModeConfirm, planMode: "off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selection := claudePermissionSelectionFromNative(tc.nativeMode)
			if selection.AccessMode != tc.accessMode || selection.PlanMode != tc.planMode {
				t.Fatalf("unexpected native selection for %q: %#v", tc.nativeMode, selection)
			}
		})
	}
}

func TestRuntimeStateSnapshotProjectsNativePermissionMode(t *testing.T) {
	tr := NewTranslator("inst-1")
	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "session-claude-1",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "bypassPermissions",
	})

	snapshot := tr.RuntimeStateSnapshot()
	if snapshot.NativePermissionMode != "bypassPermissions" {
		t.Fatalf("expected native permission mode in snapshot, got %#v", snapshot)
	}
	if snapshot.AccessMode != agentproto.AccessModeFullAccess || snapshot.PlanMode != "off" {
		t.Fatalf("unexpected projected snapshot modes: %#v", snapshot)
	}
}

func TestListSessionThreadsProjectsPermissionModes(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	workspaceRoot := filepath.Join(t.TempDir(), "ws-mode")
	writeClaudeSessionFile(t, configDir, workspaceRoot, "session-full", time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC), []map[string]any{
		{"type": "system", "cwd": workspaceRoot, "session_id": "session-full", "model": "mimo-v2.5-pro", "permissionMode": "bypassPermissions"},
		{"type": "session-title", "title": "Full session"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Workspace full prompt"}},
	})
	writeClaudeSessionFile(t, configDir, workspaceRoot, "session-plan", time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC), []map[string]any{
		{"type": "system", "cwd": workspaceRoot, "session_id": "session-plan", "permissionMode": "plan"},
		{"type": "session-title", "title": "Plan session"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Workspace plan prompt"}},
	})

	threads, err := listSessionThreads(workspaceRoot, false, RuntimeStateSnapshot{})
	if err != nil {
		t.Fatalf("listSessionThreads: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %#v", threads)
	}
	if threads[0].ThreadID != "session-plan" || threads[0].PlanMode != "on" {
		t.Fatalf("expected session-plan to project plan mode, got %#v", threads[0])
	}
	if threads[1].ThreadID != "session-full" || threads[1].PlanMode != "off" {
		t.Fatalf("expected session-full to project non-plan mode, got %#v", threads[1])
	}
}
