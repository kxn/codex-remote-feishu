package claude

import (
	"testing"

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
