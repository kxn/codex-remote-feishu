package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestBuildCatalogContextDefaultsToCodexDetached(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	ctx := svc.buildCatalogContext(svc.root.Surfaces["surface-1"])
	if ctx.Backend != agentproto.BackendCodex {
		t.Fatalf("Backend = %q, want %q", ctx.Backend, agentproto.BackendCodex)
	}
	if ctx.ProductMode != string(state.ProductModeNormal) {
		t.Fatalf("ProductMode = %q, want %q", ctx.ProductMode, state.ProductModeNormal)
	}
	if ctx.MenuStage != string(control.FeishuCommandMenuStageDetached) {
		t.Fatalf("MenuStage = %q, want %q", ctx.MenuStage, control.FeishuCommandMenuStageDetached)
	}
	if ctx.AttachedKind != string(control.CatalogAttachedKindDetached) {
		t.Fatalf("AttachedKind = %q, want %q", ctx.AttachedKind, control.CatalogAttachedKindDetached)
	}
	if ctx.InstanceID != "" || ctx.WorkspaceKey != "" {
		t.Fatalf("expected detached context without instance/workspace, got %#v", ctx)
	}
	if !ctx.Capabilities.ThreadsRefresh || !ctx.Capabilities.TurnSteer || !ctx.Capabilities.VSCodeMode {
		t.Fatalf("expected codex fallback capabilities, got %#v", ctx.Capabilities)
	}
}

func TestBuildCatalogContextUsesDetachedSurfaceBackend(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].Backend = agentproto.BackendClaude

	ctx := svc.buildCatalogContext(svc.root.Surfaces["surface-1"])
	if ctx.Backend != agentproto.BackendClaude {
		t.Fatalf("Backend = %q, want %q", ctx.Backend, agentproto.BackendClaude)
	}
	if ctx.ProductMode != string(state.ProductModeNormal) {
		t.Fatalf("ProductMode = %q, want %q", ctx.ProductMode, state.ProductModeNormal)
	}
	if !ctx.Capabilities.RequestRespond {
		t.Fatalf("expected claude fallback capabilities, got %#v", ctx.Capabilities)
	}
	if !ctx.Capabilities.ThreadsRefresh || !ctx.Capabilities.SessionCatalog || !ctx.Capabilities.ResumeByThreadID || !ctx.Capabilities.RequiresCWDForResume {
		t.Fatalf("expected claude catalog/history capabilities on detached context: %#v", ctx.Capabilities)
	}
	if ctx.Capabilities.TurnSteer || ctx.Capabilities.VSCodeMode {
		t.Fatalf("unexpected unsupported capabilities on detached claude context: %#v", ctx.Capabilities)
	}
}

func TestBuildCatalogContextUsesAttachedInstanceRuntimeSeam(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-claude",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-claude"

	ctx := svc.buildCatalogContext(svc.root.Surfaces["surface-1"])
	if ctx.Backend != agentproto.BackendClaude {
		t.Fatalf("Backend = %q, want %q", ctx.Backend, agentproto.BackendClaude)
	}
	if ctx.ProductMode != string(state.ProductModeVSCode) {
		t.Fatalf("ProductMode = %q, want %q", ctx.ProductMode, state.ProductModeVSCode)
	}
	if ctx.MenuStage != string(control.FeishuCommandMenuStageVSCodeWorking) {
		t.Fatalf("MenuStage = %q, want %q", ctx.MenuStage, control.FeishuCommandMenuStageVSCodeWorking)
	}
	if ctx.AttachedKind != string(control.CatalogAttachedKindInstance) {
		t.Fatalf("AttachedKind = %q, want %q", ctx.AttachedKind, control.CatalogAttachedKindInstance)
	}
	if ctx.InstanceID != "inst-claude" {
		t.Fatalf("InstanceID = %q, want inst-claude", ctx.InstanceID)
	}
	if ctx.WorkspaceKey != "/data/dl/repo" {
		t.Fatalf("WorkspaceKey = %q, want /data/dl/repo", ctx.WorkspaceKey)
	}
	if !ctx.Capabilities.RequestRespond {
		t.Fatalf("expected claude effective capabilities, got %#v", ctx.Capabilities)
	}
	if !ctx.Capabilities.ThreadsRefresh || !ctx.Capabilities.SessionCatalog || !ctx.Capabilities.ResumeByThreadID || !ctx.Capabilities.RequiresCWDForResume {
		t.Fatalf("expected claude catalog/history capabilities on attached context: %#v", ctx.Capabilities)
	}
	if ctx.Capabilities.TurnSteer || ctx.Capabilities.VSCodeMode {
		t.Fatalf("unexpected unsupported capabilities on claude context: %#v", ctx.Capabilities)
	}
}

func TestBuildCatalogContextUsesExplicitAttachedInstanceCapabilities(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:           "inst-claude-skeleton",
		WorkspaceRoot:        "/data/dl/repo",
		WorkspaceKey:         "/data/dl/repo",
		Backend:              agentproto.BackendClaude,
		CapabilitiesDeclared: true,
		Capabilities:         agentproto.Capabilities{},
		Online:               true,
		Threads:              map[string]*state.ThreadRecord{},
	})
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-claude-skeleton"

	ctx := svc.buildCatalogContext(svc.root.Surfaces["surface-1"])
	if ctx.Backend != agentproto.BackendClaude {
		t.Fatalf("Backend = %q, want %q", ctx.Backend, agentproto.BackendClaude)
	}
	if ctx.Capabilities.ThreadsRefresh || ctx.Capabilities.TurnSteer || ctx.Capabilities.RequestRespond || ctx.Capabilities.SessionCatalog || ctx.Capabilities.ResumeByThreadID || ctx.Capabilities.RequiresCWDForResume || ctx.Capabilities.VSCodeMode {
		t.Fatalf("expected explicit attached capabilities to stay zero, got %#v", ctx.Capabilities)
	}
}
