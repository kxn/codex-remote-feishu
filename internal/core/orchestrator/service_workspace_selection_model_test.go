package orchestrator

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
	if view.Source != control.TargetPickerRequestSourceList || view.Title != "切换工作区与会话" {
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

func TestListWorkspacesDeduplicatesPersistedWorkspaceAliases(t *testing.T) {
	now := time.Date(2026, 4, 14, 9, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	baseDir := t.TempDir()
	workspaceRoot := filepath.Join(baseDir, "proj")
	t.Chdir(baseDir)

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "proj",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "proj",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID:   "thread-1",
				Name:       "会话-1",
				CWD:        workspaceRoot,
				LastUsedAt: now,
			},
		},
	})
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recentWorkspaces: map[string]time.Time{
			"./proj": now,
		},
	})

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
	if len(view.WorkspaceOptions) != 1 {
		t.Fatalf("expected persisted workspace alias to collapse into one option, got %#v", view.WorkspaceOptions)
	}
	if !testutil.SamePath(view.WorkspaceOptions[0].Value, workspaceRoot) {
		t.Fatalf("workspace option value = %q, want %q", view.WorkspaceOptions[0].Value, workspaceRoot)
	}
}

func TestBuildWorkspaceSelectionModelFiltersNormalModeByClaudeBackend(t *testing.T) {
	now := time.Date(2026, 4, 29, 3, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-claude",
		DisplayName:   "claude-repo",
		WorkspaceRoot: "/data/dl/claude",
		WorkspaceKey:  "/data/dl/claude",
		ShortName:     "claude",
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-claude": {ThreadID: "thread-claude", Name: "Claude 会话", CWD: "/data/dl/claude", LastUsedAt: now},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-codex",
		DisplayName:   "codex-repo",
		WorkspaceRoot: "/data/dl/codex",
		WorkspaceKey:  "/data/dl/codex",
		ShortName:     "codex",
		Backend:       agentproto.BackendCodex,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-codex": {ThreadID: "thread-codex", Name: "Codex 会话", CWD: "/data/dl/codex", LastUsedAt: now.Add(-1 * time.Minute)},
		},
	})
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recentWorkspaces: map[string]time.Time{
			"/data/dl/codex": now.Add(-2 * time.Minute),
		},
	})

	model, events := svc.buildWorkspaceSelectionModel(svc.root.Surfaces["surface-1"], 1)
	if len(events) != 0 || model == nil {
		t.Fatalf("expected workspace selection model, got model=%#v events=%#v", model, events)
	}
	if len(model.Entries) != 1 || !testutil.SamePath(model.Entries[0].WorkspaceKey, "/data/dl/claude") {
		t.Fatalf("expected claude backend workspace list only, got %#v", model.Entries)
	}
}

func TestBuildWorkspaceSelectionModelDoesNotFilterClaudeWorkspaceByProfile(t *testing.T) {
	now := time.Date(2026, 4, 29, 3, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "profile-a", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-claude",
		DisplayName:     "claude-repo",
		WorkspaceRoot:   "/data/dl/claude",
		WorkspaceKey:    "/data/dl/claude",
		ShortName:       "claude",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "profile-b",
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-claude": {ThreadID: "thread-claude", Name: "Claude 会话", CWD: "/data/dl/claude", LastUsedAt: now},
		},
	})

	model, events := svc.buildWorkspaceSelectionModel(svc.root.Surfaces["surface-1"], 1)
	if len(events) != 0 || model == nil {
		t.Fatalf("expected workspace selection model, got model=%#v events=%#v", model, events)
	}
	if len(model.Entries) != 1 || !testutil.SamePath(model.Entries[0].WorkspaceKey, "/data/dl/claude") {
		t.Fatalf("expected profile-mismatched claude workspace to stay visible, got %#v", model.Entries)
	}
}

func TestBuildWorkspaceSelectionModelDoesNotFilterCodexWorkspaceByProvider(t *testing.T) {
	now := time.Date(2026, 5, 1, 2, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendCodex, "team-proxy", "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-codex",
		DisplayName:     "codex-repo",
		WorkspaceRoot:   "/data/dl/codex",
		WorkspaceKey:    "/data/dl/codex",
		ShortName:       "codex",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "default",
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-codex": {ThreadID: "thread-codex", Name: "Codex 会话", CWD: "/data/dl/codex", LastUsedAt: now},
		},
	})

	model, events := svc.buildWorkspaceSelectionModel(svc.root.Surfaces["surface-1"], 1)
	if len(events) != 0 || model == nil {
		t.Fatalf("expected workspace selection model, got model=%#v events=%#v", model, events)
	}
	if len(model.Entries) != 1 || !testutil.SamePath(model.Entries[0].WorkspaceKey, "/data/dl/codex") {
		t.Fatalf("expected provider-mismatched codex workspace to stay visible, got %#v", model.Entries)
	}
	if model.Entries[0].Attachable {
		t.Fatalf("expected provider-mismatched codex workspace to stop pretending it is directly attachable, got %#v", model.Entries[0])
	}
}
