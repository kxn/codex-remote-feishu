package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPrivateLocalDirectoryIntentPrecedesFilesystemAndMatchesCompletion(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 49, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	root := t.TempDir()
	a := control.Action{Kind: control.ActionListInstances, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"}
	view := singleTargetPickerEvent(t, s.ApplySurfaceAction(a))
	add := openAddWorkspaceLocalDirectoryPage(t, s, view)
	record := s.activeTargetPicker(s.root.Surfaces["surface-1"])
	record.LocalDirectoryPath = root
	record.LocalDirectoryName = "child"
	a.Kind = control.ActionTargetPickerConfirm
	a.PickerID = add.PickerID
	if result := s.ApplySurfaceAction(a); len(result) != 1 || result[0].TargetPickerView == nil || !result[0].TargetPickerView.LocalDirectoryChecked {
		t.Fatalf("check: %#v", result)
	}
	events := s.ApplySurfaceAction(a)
	var command *control.DaemonCommand
	for _, event := range events {
		if event.DaemonCommand != nil {
			command = event.DaemonCommand
		}
	}
	path := filepath.Join(root, "child")
	if command == nil || command.Kind != control.DaemonCommandPrivateMkdir || command.LocalPath != path {
		t.Fatalf("intent: %#v", events)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("directory created before commit: %v", err)
	}
	if _, err := s.CompletePrivateLocalDirectory("surface-1", add.PickerID, filepath.Join(root, "wrong"), true); err == nil {
		t.Fatal("wrong path accepted")
	}
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompletePrivateLocalDirectory("surface-1", add.PickerID, path, true); err != nil {
		t.Fatal(err)
	}
}
