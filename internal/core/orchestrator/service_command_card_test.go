package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCardOwnedVerboseApplyReturnsSealedCommandCard(t *testing.T) {
	now := time.Date(2026, 4, 18, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionVerboseCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-card-1",
		Text:             "/verbose quiet",
		CatalogFamilyID:  control.FeishuCommandVerbose,
		CatalogVariantID: "verbose.codex.normal",
		CatalogBackend:   "codex",
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})
	if len(events) != 1 {
		t.Fatalf("expected sealed command card event, got %#v", events)
	}
	if !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected command card apply to request inline replacement, got %#v", events[0])
	}
	if got := svc.root.Surfaces["surface-1"].Verbosity; got != state.SurfaceVerbosityQuiet {
		t.Fatalf("expected surface verbosity quiet, got %q", got)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Interactive {
		t.Fatalf("expected sealed card to be non-interactive, got %#v", catalog)
	}
	if !catalog.Sealed {
		t.Fatalf("expected sealed catalog metadata, got %#v", catalog)
	}
	if len(catalog.BodySections) == 0 || len(catalog.NoticeSections) == 0 {
		t.Fatalf("expected config card to keep body and notice areas separate, got %#v", catalog)
	}
	summaryText := commandCatalogSummaryText(catalog)
	if !strings.Contains(summaryText, "已将当前飞书会话的前端详细程度切换为 quiet。") {
		t.Fatalf("expected sealed summary to include success text, got %q", summaryText)
	}
	if !strings.Contains(summaryText, "如需再次调整，请重新发送 /verbose。") {
		t.Fatalf("expected sealed summary to include reopen hint, got %q", summaryText)
	}
}

func TestCardOwnedModelInvalidInputStaysOnCard(t *testing.T) {
	now := time.Date(2026, 4, 18, 11, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-card-1",
		Text:             "/model gpt-5.4 wrong",
		CatalogFamilyID:  control.FeishuCommandModel,
		CatalogVariantID: "model.codex.normal",
		CatalogBackend:   "codex",
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})
	if len(events) != 1 {
		t.Fatalf("expected retryable command card event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if !catalog.Interactive {
		t.Fatalf("expected invalid input card to remain interactive, got %#v", catalog)
	}
	if len(catalog.BodySections) == 0 || len(catalog.NoticeSections) == 0 {
		t.Fatalf("expected invalid input to preserve body + notice split, got %#v", catalog)
	}
	summaryText := commandCatalogSummaryText(catalog)
	if !strings.Contains(summaryText, "推理强度建议使用") {
		t.Fatalf("expected invalid input summary, got %q", summaryText)
	}
	form := catalog.Sections[1].Entries[0].Form
	if form == nil || form.Field.DefaultValue != "gpt-5.4 wrong" {
		t.Fatalf("expected manual form to keep invalid input, got %#v", catalog.Sections[1].Entries[0])
	}
}

func TestCardOwnedReasoningApplyWithoutAttachmentShowsRecoveryCard(t *testing.T) {
	now := time.Date(2026, 4, 18, 11, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-card-1",
		Text:             "/reasoning high",
		CatalogFamilyID:  control.FeishuCommandReasoning,
		CatalogVariantID: "reasoning.codex.normal",
		CatalogBackend:   "codex",
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})
	if len(events) != 1 {
		t.Fatalf("expected recovery command card event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if !catalog.Interactive {
		t.Fatalf("expected recovery card to remain interactive, got %#v", catalog)
	}
	if len(catalog.BodySections) == 0 {
		t.Fatalf("expected recovery card to keep attachment guidance in body area, got %#v", catalog)
	}
	summaryText := commandCatalogSummaryText(catalog)
	if !strings.Contains(summaryText, "您没有接管任何工作区") || !strings.Contains(summaryText, "还没接管目标") {
		t.Fatalf("expected recovery summary to explain detached state, got %q", summaryText)
	}
	if len(catalog.Sections) != 1 || len(catalog.Sections[0].Entries) != 3 {
		t.Fatalf("expected recovery actions to remain available, got %#v", catalog.Sections)
	}
}
