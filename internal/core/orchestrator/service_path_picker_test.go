package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
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

type fakePathPickerEntryFilter struct {
	hidden map[string]bool
}

func (f *fakePathPickerConsumer) PathPickerConfirmed(_ *Service, _ *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
	f.confirmed = append(f.confirmed, result)
	return []control.UIEvent{{Kind: control.UIEventNotice, SurfaceSessionID: "surface-1", Notice: &control.Notice{Code: "consumer_confirmed", Text: result.SelectedPath}}}
}

func (f *fakePathPickerConsumer) PathPickerCancelled(_ *Service, _ *state.SurfaceConsoleRecord, result control.PathPickerResult) []control.UIEvent {
	f.cancelled = append(f.cancelled, result)
	return []control.UIEvent{{Kind: control.UIEventNotice, SurfaceSessionID: "surface-1", Notice: &control.Notice{Code: "consumer_cancelled", Text: result.RootPath}}}
}

func (f *fakePathPickerEntryFilter) PathPickerFilterEntry(_ *Service, _ *state.SurfaceConsoleRecord, _ *activePathPickerRecord, item control.FeishuPathPickerEntry, _ string) (control.FeishuPathPickerEntry, bool) {
	if f == nil || !f.hidden[strings.TrimSpace(item.Name)] {
		return item, true
	}
	item.DisabledReason = "filtered"
	return item, false
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

func pathPickerNoticeText(view *control.FeishuPathPickerView) string {
	if view == nil {
		return ""
	}
	lines := make([]string, 0, 8)
	for _, section := range view.NoticeSections {
		if label := strings.TrimSpace(section.Label); label != "" {
			lines = append(lines, label)
		}
		for _, line := range section.Lines {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				lines = append(lines, trimmed)
			}
		}
	}
	return strings.Join(lines, "\n")
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
	confirmed := singlePathPickerEvent(t, confirmEvents)
	if !confirmed.Sealed || !strings.Contains(pathPickerNoticeText(confirmed), "已确认路径") {
		t.Fatalf("expected sealed same-card confirm state, got %#v", confirmed)
	}
	if svc.activePathPicker(svc.root.Surfaces["surface-1"]) != nil {
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
	rejected := singlePathPickerEvent(t, rejectEvents)
	if rejected.Sealed || !strings.Contains(pathPickerNoticeText(rejected), "当前只可选择文件") {
		t.Fatalf("expected inline same-card file-type rejection, got %#v", rejected)
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

func TestOpenPathPickerFileModeCurrentDirectoryEntryIsNoOp(t *testing.T) {
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
		Mode:     control.PathPickerModeFile,
		RootPath: root,
	})
	view := singlePathPickerEvent(t, events)

	selectedEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      "file.txt",
	})
	selected := singlePathPickerEvent(t, selectedEvents)
	if !testutil.SamePath(selected.SelectedPath, filepath.Join(root, "file.txt")) {
		t.Fatalf("expected selected file before current-directory no-op, got %#v", selected)
	}

	noopEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerEnter,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      ".",
	})
	noop := singlePathPickerEvent(t, noopEvents)
	if !testutil.SamePath(noop.CurrentPath, root) || !testutil.SamePath(noop.SelectedPath, filepath.Join(root, "file.txt")) {
		t.Fatalf("expected current-directory entry to preserve current state, got %#v", noop)
	}
}

func TestOpenPathPickerEntryFilterHidesAndBlocksFilteredEntries(t *testing.T) {
	now := time.Date(2026, 4, 12, 20, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	for _, dir := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	svc.RegisterPathPickerEntryFilter("fake", &fakePathPickerEntryFilter{
		hidden: map[string]bool{"beta": true},
	})
	events := svc.OpenPathPicker(control.Action{
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
	}, control.PathPickerRequest{
		Mode:            control.PathPickerModeDirectory,
		RootPath:        root,
		EntryFilterKind: "fake",
	})
	view := singlePathPickerEvent(t, events)

	var directories []string
	for _, entry := range view.Entries {
		if entry.Kind == control.PathPickerEntryDirectory {
			directories = append(directories, entry.Name)
		}
	}
	if got, want := directories, []string{"alpha"}; !equalStringSlices(got, want) {
		t.Fatalf("unexpected filtered directories: got %v want %v", got, want)
	}

	blocked := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerEnter,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
		PickerEntry:      "beta",
	})
	blockedView := singlePathPickerEvent(t, blocked)
	if blockedView.Sealed || !strings.Contains(pathPickerNoticeText(blockedView), "filtered") {
		t.Fatalf("expected filtered directory to stay on same card, got %#v", blockedView)
	}
}

func TestBuildPathPickerEntriesSortsDotDirectoriesAfterNormalDirectories(t *testing.T) {
	svc := &Service{}
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

	entries, err := svc.buildPathPickerEntries(nil, &activePathPickerRecord{
		Mode:        pathPickerModeFile,
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

func TestBuildPathPickerEntriesAllowsResolvedChildrenUnderSymlinkRoot(t *testing.T) {
	svc := &Service{}
	realRoot := t.TempDir()
	linkParent := t.TempDir()
	linkRoot := filepath.Join(linkParent, "workspace-link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	for _, dir := range []string{"zeta", ".hidden", "alpha"} {
		if err := os.Mkdir(filepath.Join(realRoot, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	entries, err := svc.buildPathPickerEntries(nil, &activePathPickerRecord{
		Mode:        pathPickerModeDirectory,
		RootPath:    linkRoot,
		CurrentPath: linkRoot,
	})
	if err != nil {
		t.Fatalf("build entries through symlink root: %v", err)
	}

	var directories []string
	for _, entry := range entries {
		if entry.Kind == control.PathPickerEntryDirectory {
			directories = append(directories, entry.Name)
		}
	}
	if got, want := directories, []string{"alpha", "zeta", ".hidden"}; !equalStringSlices(got, want) {
		t.Fatalf("unexpected directory order through symlink root: got %v want %v", got, want)
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
	outsideRejected := singlePathPickerEvent(t, outsideEvents)
	if outsideRejected.Sealed || !strings.Contains(pathPickerNoticeText(outsideRejected), "目标条目无效") {
		t.Fatalf("expected out-of-root rejection to stay on same card, got %#v", outsideRejected)
	}

	escapeEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		PickerID:         view.PickerID,
		PickerEntry:      "escape.txt",
	})
	escapeRejected := singlePathPickerEvent(t, escapeEvents)
	if escapeRejected.Sealed || !strings.Contains(pathPickerNoticeText(escapeRejected), "目标条目无效") {
		t.Fatalf("expected symlink escape rejection to stay on same card, got %#v", escapeRejected)
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
	rejected := singlePathPickerEvent(t, rejectEvents)
	if rejected.Sealed || !strings.Contains(pathPickerNoticeText(rejected), "当前只可选择目录") {
		t.Fatalf("expected directory-type rejection to stay on same card, got %#v", rejected)
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
	if svc.activePathPicker(svc.root.Surfaces["surface-1"]) == nil {
		t.Fatalf("expected unauthorized action to preserve active picker")
	}

	ownerCancelEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerCancel,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	cancelled := singlePathPickerEvent(t, ownerCancelEvents)
	if !cancelled.Sealed || !strings.Contains(pathPickerNoticeText(cancelled), "已取消路径选择") {
		t.Fatalf("expected owner cancel to seal same card after unauthorized attempt, got %#v", cancelled)
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
	if svc.activePathPicker(svc.root.Surfaces["surface-1"]) != nil {
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
	if svc.activePathPicker(svc.root.Surfaces["surface-1"]) != nil {
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
	cancelled := singlePathPickerEvent(t, cancelEvents)
	if !cancelled.Sealed || !strings.Contains(pathPickerNoticeText(cancelled), "已取消路径选择") {
		t.Fatalf("expected cancel to seal same card, got %#v", cancelled)
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
	confirmed := singlePathPickerEvent(t, confirmEvents)
	if !confirmed.Sealed || !strings.Contains(pathPickerNoticeText(confirmed), filepath.Join(root, "file.txt")) {
		t.Fatalf("expected consumer confirm to seal same card with notice, got %#v", confirmed)
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
	if svc.activePathPicker(svc.root.Surfaces["surface-1"]) != nil {
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
	cancelled := singlePathPickerEvent(t, cancelEvents)
	if !cancelled.Sealed || !strings.Contains(pathPickerNoticeText(cancelled), root) {
		t.Fatalf("expected consumer cancel to seal same card with notice, got %#v", cancelled)
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
	confirmed := singlePathPickerEvent(t, confirmEvents)
	if !confirmed.Sealed || !strings.Contains(pathPickerNoticeText(confirmed), "已确认路径") {
		t.Fatalf("expected generic confirm to seal same card without business coupling, got %#v", confirmed)
	}
}
