package workservice

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestMemberWorkspaceRecoveryAndScope(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false)
	root, err := ResolveMemberWorkspaceRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WithMemberWorkspaceHost(root, "local"); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "old")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "state-alias")
	if err := os.Symlink(root, alias); err == nil {
		old = filepath.Join(alias, "old")
	} else {
		t.Logf("configured-root alias unavailable on this host: %v", err)
	}
	err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		if err := tx.PutContext(work.WorkContext{ID: "other-role-context", ScopeID: "project-scope", RoleID: "expert", Workstream: "other", Checkpoint: "secret-other-role", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutTask(work.Task{ID: "new-task", ScopeID: "project-scope", Title: "New task", OwnerRoleID: "leader", State: work.TaskTodo, RequirementsRevision: 1, AssignmentGeneration: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutContext(work.WorkContext{ID: "new-context", ScopeID: "project-scope", RoleID: "leader", TaskID: "new-task", Workstream: "new-task", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutContext(work.WorkContext{ID: "old-context", ScopeID: "project-scope", RoleID: "leader", Workstream: "old", Checkpoint: "continue old work", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutLocation(work.ExecutionLocation{ID: "old-location", HostID: "local", Cwd: old, IsolatedContextID: "old-context", Generation: 1, Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutWorker(work.WorkerSession{ID: "old-worker", ContextID: "old-context", ScopeID: "project-scope", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Controller: work.ControllerAutomatic, Generation: 1, ControllerGeneration: 1, LocationID: "old-location", LocationGeneration: 1, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	source := work.SourceRef{Kind: "message", ID: "lifecycle-human", Version: "1", EvidenceRef: "lifecycle-human-body"}
	saved, err := s.MemoryWrite(t.Context(), token, run, MemoryWriteRequest{Key: "experience", Content: "project decision", Applicability: "future work", Sources: []work.SourceRef{source}})
	if err != nil {
		t.Fatal(err)
	}
	var items []MemberWorkspaceItem
	after := ""
	for {
		page, e := s.MemberWorkspaceInspect(t.Context(), token, run, KnowledgeListRequest{Limit: 1, After: after})
		if e != nil {
			t.Fatal(e)
		}
		items = append(items, page.Items...)
		if page.NextCursor == "" {
			break
		}
		after = page.NextCursor
	}
	var foundContext, foundMemory bool
	for _, item := range items {
		if item.ID == "other-role-context" {
			t.Fatal("other member context leaked")
		}
		if item.ID == "old-context" {
			foundContext = item.Path == old && item.LocationStatus == "remembered_path_unverified" && item.Checkpoint == "continue old work"
		}
		if item.ID == saved.ObjectID {
			foundMemory = true
		}
	}
	if !foundContext || !foundMemory {
		t.Fatalf("missing recovery records: %+v", items)
	}
	page, err := s.MemberWorkspaceInspect(t.Context(), token, run, KnowledgeListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	same, _ := MemberWorkspacePath(root, "project", "leader")
	other, _ := MemberWorkspacePath(root, "other-project", "leader")
	colleague, _ := MemberWorkspacePath(root, "project", "expert")
	if page.Path != same || same == other || same == colleague {
		t.Fatalf("wrong identity paths: %q %q %q", same, other, colleague)
	}
	bundle, err := s.BuildInstructions(t.Context(), "lifecycle-context")
	if err != nil || strings.Contains(bundle.Text, same) {
		t.Fatalf("launch path missing: %v", err)
	}
	newBundle, err := s.BuildInstructions(t.Context(), "new-context")
	if err != nil || strings.Contains(newBundle.Text, same) || !strings.Contains(newBundle.Text, "project decision") {
		t.Fatalf("new context lost workspace: %v", err)
	}
	// Rebuilding a launch in another execution directory keeps member identity.
	for _, cwd := range []string{t.TempDir(), t.TempDir()} {
		if err := os.WriteFile(filepath.Join(cwd, "independent"), []byte("cwd"), 0600); err != nil {
			t.Fatal(err)
		}
		rebuilt, err := s.BuildInstructions(t.Context(), "new-context")
		if err != nil || strings.Contains(rebuilt.Text, same) {
			t.Fatalf("rebuilt launch at %s: %v", cwd, err)
		}
	}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		member, err := tx.GetTeamMember("team", "leader")
		if err != nil {
			return err
		}
		member.Active = false
		member.Revision++
		return tx.PutTeamMember(member, 1)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MemberWorkspaceInspect(t.Context(), token, run, KnowledgeListRequest{}); err == nil {
		t.Fatal("removed member retained workspace access")
	}
	private, privateToken, privateRun := knowledgeService(t)
	if err := private.WithMemberWorkspaceHost(root, "local"); err != nil {
		t.Fatal(err)
	}
	if _, err := private.MemberWorkspaceInspect(t.Context(), privateToken, privateRun, KnowledgeListRequest{}); !errors.Is(err, workstore.ErrScopeDenied) {
		t.Fatalf("private access: %v", err)
	}
}
