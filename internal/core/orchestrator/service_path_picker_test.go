package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

type fakePathPickerConsumer struct {
	confirmed []control.PathPickerResult
	cancelled []control.PathPickerResult
}

func (f *fakePathPickerConsumer) PathPickerConfirmed(_ *Service, _ *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
	f.confirmed = append(f.confirmed, result)
	return []control.UIEvent{{Kind: control.UIEventNotice, SurfaceSessionID: "surface-1", Notice: &control.Notice{Code: "consumer_confirmed", Text: result.SelectedPath}}}
}

func (f *fakePathPickerConsumer) PathPickerCancelled(_ *Service, _ *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
	f.cancelled = append(f.cancelled, result)
	return []control.UIEvent{{Kind: control.UIEventNotice, SurfaceSessionID: "surface-1", Notice: &control.Notice{Code: "consumer_cancelled", Text: result.RootPath}}}
}

func pathPickerViewFromEvent(t *testing.T, event control.UIEvent) *control.FeishuPathPickerView {
	t.Helper()
	if event.Kind != control.UIEventFeishuPathPicker || event.FeishuPathPickerView == nil {
		t.Fatalf("expected path picker event, got %#v", event)
	}
	return event.FeishuPathPickerView
}

func singlePathPickerEvent(t *testing.T, events []control.UIEvent) *control.FeishuPathPickerView {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("expected one event, got %#v", events)
	}
	return pathPickerViewFromEvent(t, events[0])
}

func TestOpenPathPickerDirectoryModeNavigatesAndConfirmsCurrentDirectory(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeDirectory,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)
	if !view.CanConfirm || !testutil.SamePath(view.CurrentPath, root) {
		t.Fatalf("unexpected initial picker view: %#v", view)
	}

	enterEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerEnter,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      "alpha",
	})
	entered := singlePathPickerEvent(t, enterEvents)
	if !testutil.SamePath(entered.CurrentPath, filepath.Join(root, "alpha")) || !entered.CanGoUp {
		t.Fatalf("unexpected entered picker view: %#v", entered)
	}

	upEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerUp,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
	})
	back := singlePathPickerEvent(t, upEvents)
	if !testutil.SamePath(back.CurrentPath, root) {
		t.Fatalf("expected to return to root, got %#v", back)
	}

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
	})
	if len(confirmEvents) != 1 || confirmEvents[0].Notice == nil || confirmEvents[0].Notice.Code != "path_picker_confirmed" {
		t.Fatalf("expected confirmed notice, got %#v", confirmEvents)
	}
	if svc.root.Surfaces["surface-1"].ActivePathPicker != nil {
		t.Fatalf("expected picker state to clear after confirm")
	}
}

func TestOpenPathPickerFileModeSelectsFileAndRejectsDirectorySelection(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeFile,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)
	if view.CanConfirm {
		t.Fatalf("expected file picker to require an explicit file selection")
	}

	selectEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      "file.txt",
	})
	selected := singlePathPickerEvent(t, selectEvents)
	if !testutil.SamePath(selected.SelectedPath, filepath.Join(root, "file.txt")) || !selected.CanConfirm {
		t.Fatalf("unexpected selected picker view: %#v", selected)
	}

	rejectEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      "subdir",
	})
	if len(rejectEvents) != 1 || rejectEvents[0].Notice == nil || rejectEvents[0].Notice.Code != "path_picker_not_file" {
		t.Fatalf("expected file-type rejection notice, got %#v", rejectEvents)
	}
}

func TestOpenPathPickerFileModeAllowsParentDirectoryEntry(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:        control.PathPickerModeFile,
		RootPath:    root,
		InitialPath: nested,
	})
	view := singlePathPickerEvent(t, events)
	if !testutil.SamePath(view.CurrentPath, nested) || !view.CanGoUp {
		t.Fatalf("expected nested file picker view, got %#v", view)
	}

	enterEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerEnter,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      "..",
	})
	back := singlePathPickerEvent(t, enterEvents)
	if !testutil.SamePath(back.CurrentPath, root) {
		t.Fatalf("expected parent entry to return to root, got %#v", back)
	}
}

func TestBuildPathPickerEntriesSortsDotDirectoriesAfterNormalDirectories(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"zeta", ".hidden", "alpha"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for _, file := range []string{"b.txt", ".env"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte("ok"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}

	entries, err := buildPathPickerEntries(&state.ActivePathPickerRecord{
		Mode:        state.PathPickerModeFile,
		RootPath:    root,
		CurrentPath: root,
	})
	if err != nil {
		t.Fatalf("build entries: %v", err)
	}

	var directories []string
	var files []string
	for _, entry := range entries {
		switch entry.Kind {
		case control.PathPickerEntryDirectory:
			directories = append(directories, entry.Name)
		case control.PathPickerEntryFile:
			files = append(files, entry.Name)
		}
	}
	if got, want := directories, []string{"alpha", "zeta", ".hidden"}; !equalStringSlices(got, want) {
		t.Fatalf("unexpected directory order: got %v want %v", got, want)
	}
	if got, want := files, []string{".env", "b.txt"}; !equalStringSlices(got, want) {
		t.Fatalf("unexpected file order: got %v want %v", got, want)
	}
}

func TestOpenPathPickerRejectsPathEscapesAndSymlinkEscapes(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write inside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "outside.txt"), []byte("no"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(filepath.Join(outsideDir, "outside.txt"), filepath.Join(root, "escape.txt")); err != nil {
		t.Fatalf("symlink escape: %v", err)
	}
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeFile,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)

	outsideEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      "../outside.txt",
	})
	if len(outsideEvents) != 1 || outsideEvents[0].Notice == nil || outsideEvents[0].Notice.Code != "path_picker_invalid_entry" {
		t.Fatalf("expected out-of-root rejection notice, got %#v", outsideEvents)
	}

	escapeEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      "escape.txt",
	})
	if len(escapeEvents) != 1 || escapeEvents[0].Notice == nil || escapeEvents[0].Notice.Code != "path_picker_invalid_entry" {
		t.Fatalf("expected symlink escape rejection notice, got %#v", escapeEvents)
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestOpenPathPickerDirectoryModeRejectsFileSelection(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeDirectory,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)
	rejectEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      "file.txt",
	})
	if len(rejectEvents) != 1 || rejectEvents[0].Notice == nil || rejectEvents[0].Notice.Code != "path_picker_not_directory" {
		t.Fatalf("expected directory-type rejection notice, got %#v", rejectEvents)
	}
}

func TestOpenPathPickerRejectsStalePickerID(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeDirectory,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)
	events = svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeDirectory,
		RootPath: root,
	})
	latest := singlePathPickerEvent(t, events)
	rejectEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerUp,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
	})
	if len(rejectEvents) != 1 || rejectEvents[0].Notice == nil || rejectEvents[0].Notice.Code != "path_picker_expired" {
		t.Fatalf("expected stale picker rejection, got %#v", rejectEvents)
	}
	if latest.PickerID == view.PickerID {
		t.Fatalf("expected new picker id")
	}
}

func TestPathPickerRejectsNonOwnerAndPreservesGate(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "demo",
		WorkspaceRoot: "/tmp/demo",
		WorkspaceKey:  "/tmp/demo",
		Threads:       map[string]*state.ThreadRecord{},
		Online:        true,
	})
	root := t.TempDir()
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeDirectory,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)
	rejectEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerCancel,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-2",
		PickerID:         view.PickerID,
	})
	if len(rejectEvents) != 1 || rejectEvents[0].Notice == nil || rejectEvents[0].Notice.Code != "path_picker_unauthorized" {
		t.Fatalf("expected unauthorized notice, got %#v", rejectEvents)
	}
	if svc.root.Surfaces["surface-1"].ActivePathPicker == nil {
		t.Fatalf("expected unauthorized action to preserve active picker")
	}

	ownerCancelEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerCancel,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(ownerCancelEvents) != 1 || ownerCancelEvents[0].Notice == nil || ownerCancelEvents[0].Notice.Code != "path_picker_cancelled" {
		t.Fatalf("expected owner cancel to still succeed after unauthorized attempt, got %#v", ownerCancelEvents)
	}
}

func TestPathPickerExpiresAndClearsGate(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:        control.PathPickerModeDirectory,
		RootPath:    root,
		ExpireAfter: time.Second,
	})
	view := singlePathPickerEvent(t, events)
	now = now.Add(2 * time.Second)
	expiredEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerUp,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(expiredEvents) != 1 || expiredEvents[0].Notice == nil || expiredEvents[0].Notice.Code != "path_picker_expired" {
		t.Fatalf("expected expired notice, got %#v", expiredEvents)
	}
	if svc.root.Surfaces["surface-1"].ActivePathPicker != nil {
		t.Fatalf("expected expired picker to clear active state")
	}
}

func TestPathPickerExpiredGateAutoClearsOnRouteAction(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "demo",
		WorkspaceRoot: root,
		WorkspaceKey:  root,
		Threads:       map[string]*state.ThreadRecord{},
		Online:        true,
	})
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:        control.PathPickerModeDirectory,
		RootPath:    root,
		ExpireAfter: time.Second,
	})
	view := singlePathPickerEvent(t, events)
	if view == nil {
		t.Fatal("expected picker view")
	}
	now = now.Add(2 * time.Second)
	listEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	})
	if len(listEvents) != 1 || listEvents[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected /list to proceed after picker expiry, got %#v", listEvents)
	}
	if svc.root.Surfaces["surface-1"].ActivePathPicker != nil {
		t.Fatalf("expected expired picker gate to be cleared on route action")
	}
}

func TestPathPickerBlocksRouteMutationUntilCancelled(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "demo",
		WorkspaceRoot: root,
		WorkspaceKey:  root,
		Threads:       map[string]*state.ThreadRecord{},
		Online:        true,
	})
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeDirectory,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)

	blockedList := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	})
	if len(blockedList) != 1 || blockedList[0].Notice == nil || blockedList[0].Notice.Code != "path_picker_active" {
		t.Fatalf("expected path picker gate notice, got %#v", blockedList)
	}

	statusEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	})
	if len(statusEvents) != 1 || statusEvents[0].Snapshot == nil || statusEvents[0].Snapshot.Gate.Kind != "path_picker" {
		t.Fatalf("expected status snapshot to show path picker gate, got %#v", statusEvents)
	}

	cancelEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerCancel,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(cancelEvents) != 1 || cancelEvents[0].Notice == nil || cancelEvents[0].Notice.Code != "path_picker_cancelled" {
		t.Fatalf("expected cancel notice, got %#v", cancelEvents)
	}

	listEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	})
	if len(listEvents) != 1 || listEvents[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected /list to work again after cancel, got %#v", listEvents)
	}
}

func TestPathPickerBlocksOtherFeishuCardsWhileActive(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeDirectory,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)
	_ = view
	blockedEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		Text:             "/mode",
	})
	if len(blockedEvents) != 1 || blockedEvents[0].Notice == nil || blockedEvents[0].Notice.Code != "path_picker_active" {
		t.Fatalf("expected picker gate to block other feishu cards, got %#v", blockedEvents)
	}
}

func TestPathPickerConfirmHandsValidatedResultToConsumer(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	consumer := &fakePathPickerConsumer{}
	svc.RegisterPathPickerConsumer("fake", consumer)
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:         control.PathPickerModeFile,
		RootPath:     root,
		ConsumerKind: "fake",
		ConsumerMeta: map[string]string{"flow": "send_file"},
	})
	view := singlePathPickerEvent(t, events)
	_ = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
		PickerEntry:      "file.txt",
	})
	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(confirmEvents) != 1 || confirmEvents[0].Notice == nil || confirmEvents[0].Notice.Code != "consumer_confirmed" {
		t.Fatalf("expected consumer confirm event, got %#v", confirmEvents)
	}
	if len(consumer.confirmed) != 1 {
		t.Fatalf("expected one consumer confirm call, got %#v", consumer.confirmed)
	}
	result := consumer.confirmed[0]
	if !testutil.SamePath(result.SelectedPath, filepath.Join(root, "file.txt")) || !testutil.SamePath(result.RootPath, root) || result.Mode != control.PathPickerModeFile {
		t.Fatalf("unexpected consumer confirm result: %#v", result)
	}
	if result.ConsumerMeta["flow"] != "send_file" || result.OwnerUserID != "user-1" {
		t.Fatalf("unexpected consumer confirm metadata: %#v", result)
	}
	if svc.root.Surfaces["surface-1"].ActivePathPicker != nil {
		t.Fatalf("expected confirm to clear active picker before consumer handoff")
	}
}

func TestPathPickerCancelHandsControlToConsumer(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	consumer := &fakePathPickerConsumer{}
	svc.RegisterPathPickerConsumer("fake", consumer)
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:         control.PathPickerModeDirectory,
		RootPath:     root,
		ConsumerKind: "fake",
	})
	view := singlePathPickerEvent(t, events)
	cancelEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerCancel,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(cancelEvents) != 1 || cancelEvents[0].Notice == nil || cancelEvents[0].Notice.Code != "consumer_cancelled" {
		t.Fatalf("expected consumer cancel event, got %#v", cancelEvents)
	}
	if len(consumer.cancelled) != 1 || !testutil.SamePath(consumer.cancelled[0].RootPath, root) {
		t.Fatalf("unexpected consumer cancel result: %#v", consumer.cancelled)
	}
}

func TestPathPickerWithoutConsumerStaysBusinessAgnostic(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:     control.PathPickerModeDirectory,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)
	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(confirmEvents) != 1 || confirmEvents[0].Notice == nil || confirmEvents[0].Notice.Code != "path_picker_confirmed" {
		t.Fatalf("expected generic confirm notice without business coupling, got %#v", confirmEvents)
	}
}
