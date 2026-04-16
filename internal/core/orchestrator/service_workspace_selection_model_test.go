package orchestrator

import (
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestListWorkspacesShowsPagedEntries(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("/data/dl/proj-%d", i)
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: key,
			WorkspaceKey:  key,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        key,
					LastUsedAt: now.Add(-time.Duration(i) * time.Minute),
				},
			},
		})
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceList || view.Title != "选择工作区与会话" {
		t.Fatalf("unexpected target picker title: %#v", view)
	}
	if len(view.WorkspaceOptions) != 6 {
		t.Fatalf("expected all workspaces in a single target picker, got %#v", view.WorkspaceOptions)
	}
}

func TestBuildWorkspaceSelectionModelKeepsSemanticEntries(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("/data/dl/proj-%d", i)
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: key,
			WorkspaceKey:  key,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        key,
					LastUsedAt: now.Add(-time.Duration(i) * time.Minute),
				},
			},
		})
	}

	model, events := svc.buildWorkspaceSelectionModel(svc.ensureSurface(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}), 1)
	if len(events) != 0 || model == nil {
		t.Fatalf("expected workspace selection model, got model=%#v events=%#v", model, events)
	}
	if model.Page != 1 || model.PageSize != workspaceSelectionPageSize || model.TotalPages != 1 {
		t.Fatalf("unexpected workspace selection view metadata: %#v", model)
	}
	if len(model.Entries) != 6 {
		t.Fatalf("expected semantic entries for all workspaces, got %#v", model.Entries)
	}
	if !testutil.SamePath(model.Entries[0].WorkspaceKey, "/data/dl/proj-0") || !model.Entries[0].Attachable || model.Entries[0].RecoverableOnly {
		t.Fatalf("unexpected first workspace entry: %#v", model.Entries[0])
	}
}

func TestBuildWorkspaceSelectionModelIncludesRecoverableWorkspaceOutsideInstanceRoot(t *testing.T) {
	now := time.Date(2026, 4, 14, 9, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "fschannel",
		WorkspaceRoot: "/workspace/fschannel",
		WorkspaceKey:  "/workspace/fschannel",
		ShortName:     "fschannel",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-root": {
				ThreadID:   "thread-root",
				Name:       "当前仓库",
				CWD:        "/workspace/fschannel",
				LastUsedAt: now.Add(-1 * time.Minute),
			},
			"thread-picdetect": {
				ThreadID:   "thread-picdetect",
				Name:       "picdetect",
				CWD:        "/data/dl/picdetect",
				LastUsedAt: now,
			},
		},
	})

	model, events := svc.buildWorkspaceSelectionModel(svc.ensureSurface(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}), 1)
	if len(events) != 0 || model == nil {
		t.Fatalf("expected workspace selection model, got model=%#v events=%#v", model, events)
	}

	var rootEntry *control.FeishuWorkspaceSelectionEntry
	var recoverableEntry *control.FeishuWorkspaceSelectionEntry
	for i := range model.Entries {
		entry := &model.Entries[i]
		switch {
		case testutil.SamePath(entry.WorkspaceKey, "/workspace/fschannel"):
			rootEntry = entry
		case testutil.SamePath(entry.WorkspaceKey, "/data/dl/picdetect"):
			recoverableEntry = entry
		}
	}
	if rootEntry == nil || !rootEntry.Attachable || rootEntry.RecoverableOnly {
		t.Fatalf("expected root workspace to stay attachable, got %#v", model.Entries)
	}
	if recoverableEntry == nil {
		t.Fatalf("expected recoverable workspace outside instance root to be listed, got %#v", model.Entries)
	}
	if recoverableEntry.Attachable || !recoverableEntry.RecoverableOnly {
		t.Fatalf("expected out-of-root workspace to be recoverable-only, got %#v", recoverableEntry)
	}
}

func TestBuildWorkspaceSelectionModelUsesPersistedWorkspaceAggregationBeyondThreadLimit(t *testing.T) {
	now := time.Date(2026, 4, 14, 9, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	recent := make([]state.ThreadRecord, 0, persistedRecentThreadLimit+1)
	for i := 0; i < persistedRecentThreadLimit; i++ {
		recent = append(recent, state.ThreadRecord{
			ThreadID:   fmt.Sprintf("thread-hot-%d", i),
			Name:       "hot workspace",
			CWD:        "/data/dl/hot",
			LastUsedAt: now.Add(-time.Duration(i) * time.Second),
		})
	}
	recent = append(recent, state.ThreadRecord{
		ThreadID:   "thread-legacy",
		Name:       "legacy workspace",
		CWD:        "/data/dl/legacy",
		LastUsedAt: now.Add(-24 * time.Hour),
	})
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: recent,
		recentWorkspaces: map[string]time.Time{
			"/data/dl/hot":    now,
			"/data/dl/legacy": now.Add(-24 * time.Hour),
		},
		byID: map[string]state.ThreadRecord{},
	})

	model, events := svc.buildWorkspaceSelectionModel(svc.ensureSurface(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}), 1)
	if len(events) != 0 || model == nil {
		t.Fatalf("expected workspace selection model, got model=%#v events=%#v", model, events)
	}

	foundLegacy := false
	for i := range model.Entries {
		entry := model.Entries[i]
		if !testutil.SamePath(entry.WorkspaceKey, "/data/dl/legacy") {
			continue
		}
		foundLegacy = true
		if !entry.RecoverableOnly || entry.Attachable {
			t.Fatalf("expected legacy workspace to stay recoverable-only, got %#v", entry)
		}
	}
	if !foundLegacy {
		t.Fatalf("expected legacy workspace to remain visible via workspace aggregation, got %#v", model.Entries)
	}
}
