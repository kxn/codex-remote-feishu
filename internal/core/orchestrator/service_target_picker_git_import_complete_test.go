package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestCompleteTargetPickerGitImportEntersNewThreadReadyAndClearsPicker(t *testing.T) {
	now := time.Date(2026, 4, 16, 18, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	surface.ActiveTargetPicker = &state.ActiveTargetPickerRecord{
		PickerID:     "picker-1",
		OwnerUserID:  "user-1",
		Source:       control.TargetPickerRequestSourceList,
		SelectedMode: control.FeishuTargetPickerModeAddWorkspace,
	}

	events := svc.CompleteTargetPickerGitImport("surface-1", "picker-1", workspaceRoot)

	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, workspaceRoot) {
		t.Fatalf("expected git import completion to enter new-thread-ready, got %#v", surface)
	}
	if surface.ActiveTargetPicker != nil {
		t.Fatalf("expected successful git import completion to clear target picker, got %#v", surface.ActiveTargetPicker)
	}
	var sawReady bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawReady = true
		}
	}
	if !sawReady {
		t.Fatalf("expected new-thread-ready notice after git import completion, got %#v", events)
	}
}

func TestCompleteTargetPickerGitImportReportsStaleFlowAndKeepsDirectory(t *testing.T) {
	now := time.Date(2026, 4, 16, 18, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()

	svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	events := svc.CompleteTargetPickerGitImport("surface-1", "picker-1", workspaceRoot)
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "git_import_flow_stale" {
		t.Fatalf("expected stale-flow notice, got %#v", events)
	}
	if got := events[0].Notice.Text; got == "" || !containsAll(got, normalizeWorkspaceClaimKey(workspaceRoot), "目录会保留") {
		t.Fatalf("expected stale-flow notice to mention kept directory, got %#v", events[0].Notice)
	}
}

func TestCompleteTargetPickerGitImportAttachFailureMentionsDirectoryPreserved(t *testing.T) {
	now := time.Date(2026, 4, 16, 18, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-owner",
		DisplayName:   "owner",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "owner",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	ownerSurface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-owner",
		ChatID:           "chat-owner",
		ActorUserID:      "user-owner",
	})
	svc.attachWorkspace(ownerSurface, workspaceRoot)

	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	surface.ActiveTargetPicker = &state.ActiveTargetPickerRecord{
		PickerID:     "picker-1",
		OwnerUserID:  "user-1",
		Source:       control.TargetPickerRequestSourceList,
		SelectedMode: control.FeishuTargetPickerModeAddWorkspace,
	}

	events := svc.CompleteTargetPickerGitImport("surface-1", "picker-1", workspaceRoot)

	var sawBusy bool
	var sawAttachFailed bool
	for _, event := range events {
		if event.Notice == nil {
			continue
		}
		switch event.Notice.Code {
		case "workspace_busy":
			sawBusy = true
		case "git_import_workspace_attach_failed":
			sawAttachFailed = true
			if !containsAll(event.Notice.Text, normalizeWorkspaceClaimKey(workspaceRoot), "目录已保留") {
				t.Fatalf("expected attach-failed notice to mention preserved directory, got %#v", event.Notice)
			}
		}
	}
	if !sawBusy || !sawAttachFailed {
		t.Fatalf("expected busy + attach-failed notices, got %#v", events)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
