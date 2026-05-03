package orchestrator

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestFinalizeDetachedSurfaceSealsWorkspacePageAndClearsIdleReviewSession(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
	}
	svc.workspacePageEvent(surface, control.FeishuCommandWorkspace, true, "om-workspace-1")

	events := svc.finalizeDetachedSurface(surface)

	if surface.AttachedInstanceID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected detached unbound surface, got %#v", surface)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected detach to clear review session, got %#v", surface.ReviewSession)
	}
	if svc.activeWorkspacePage(surface) != nil {
		t.Fatalf("expected workspace page runtime to clear, got %#v", svc.activeWorkspacePage(surface))
	}

	var page *control.FeishuPageView
	for _, event := range events {
		if event.PageView != nil && strings.TrimSpace(event.PageView.MessageID) == "om-workspace-1" {
			page = commandCatalogFromEvent(t, event)
			break
		}
	}
	if page == nil {
		t.Fatalf("expected detached workspace page replacement, got %#v", events)
	}
	if !page.Sealed {
		t.Fatalf("expected sealed workspace page, got %#v", page)
	}
	if text := commandCatalogSummaryText(page); !strings.Contains(text, "工作目标已断开") || !strings.Contains(text, "工作区页面已失效") {
		t.Fatalf("expected detached workspace page summary, got %q", text)
	}
}

func TestFinalizeDetachedSurfaceSealsVisiblePathPickerAndClearsHiddenTargetPicker(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)

	targetEvents := svc.openTargetPicker(surface, control.TargetPickerRequestSourceDir, "", "", "", false)
	targetView := singleTargetPickerEvent(t, targetEvents)
	svc.RecordTargetPickerMessage(surface.SurfaceSessionID, targetView.PickerID, "om-target-picker-1")
	record := svc.activeTargetPicker(surface)
	if record == nil {
		t.Fatal("expected active target picker")
	}

	pathEvents := svc.openTargetPickerAddWorkspacePathPicker(surface, record, control.FeishuTargetPickerPathFieldLocalDirectory)
	pathView := singlePathPickerEvent(t, pathEvents)
	_ = pathView

	events := svc.finalizeDetachedSurface(surface)

	if svc.activePathPicker(surface) != nil {
		t.Fatalf("expected detached surface to clear path picker runtime, got %#v", svc.activePathPicker(surface))
	}
	if svc.activeTargetPicker(surface) != nil {
		t.Fatalf("expected detached surface to clear target picker runtime, got %#v", svc.activeTargetPicker(surface))
	}

	var pathCount, targetCount int
	var sealedPath *control.FeishuPathPickerView
	for _, event := range events {
		if event.PathPickerView != nil {
			pathCount++
			sealedPath = pathPickerViewFromEvent(t, event)
		}
		if event.TargetPickerView != nil {
			targetCount++
		}
	}
	if pathCount != 1 || sealedPath == nil {
		t.Fatalf("expected one sealed path picker replacement, got %#v", events)
	}
	if targetCount != 0 {
		t.Fatalf("expected visible child picker to suppress parent target-picker replacement, got %#v", events)
	}
	if strings.TrimSpace(sealedPath.MessageID) != "om-target-picker-1" || !sealedPath.Sealed {
		t.Fatalf("expected sealed path picker update on owner-card message, got %#v", sealedPath)
	}
	if notice := pathPickerNoticeText(sealedPath); !strings.Contains(notice, "路径选择器已失效") {
		t.Fatalf("expected invalidated path picker notice, got %q", notice)
	}
}

func TestUseThreadBlockedWhileReviewTurnRunning(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	svc.root.Instances["inst-1"].Threads["thread-alt"] = &state.ThreadRecord{
		ThreadID: "thread-alt",
		Name:     "备用线程",
		CWD:      repoRoot,
		Loaded:   true,
	}
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	context := svc.buildFeishuUISurfaceContext(surface)
	if !context.RouteMutationBlocked || context.RouteMutationBlockedBy != "review_running" {
		t.Fatalf("expected surface context to expose running-review route gate, got %#v", context)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: surface.SurfaceSessionID,
		ThreadID:         "thread-alt",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "review_route_mutation_running" {
		t.Fatalf("expected running review to block thread switch, got %#v", events)
	}
	if surface.SelectedThreadID != "thread-main" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected blocked route mutation to preserve current selection, got %#v", surface)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.ActiveTurnID != "turn-review-1" {
		t.Fatalf("expected running review session to remain active, got %#v", surface.ReviewSession)
	}
}

func findEventByKind(events []eventcontract.Event, predicate func(eventcontract.Event) bool) *eventcontract.Event {
	for i := range events {
		if predicate(events[i]) {
			return &events[i]
		}
	}
	return nil
}
