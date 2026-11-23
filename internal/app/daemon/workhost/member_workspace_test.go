package workhost

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
)

func TestMemberWorkspaceStableAndRejectsSymlinks(t *testing.T) {
	root, err := workservice.ResolveMemberWorkspaceRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMemberWorkspace(root, "project", "role"); err != nil {
		t.Fatal(err)
	}
	path, _ := workservice.MemberWorkspacePath(root, "project", "role")
	file := filepath.Join(path, "notes.txt")
	if err := os.WriteFile(file, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMemberWorkspace(root, "project", "role"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "retained" {
		t.Fatalf("lost data: %q %v", b, err)
	}
	bad := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(bad, "team-member-workspaces")); err != nil {
		t.Skip(err)
	}
	if _, err := prepareMemberWorkspace(bad, "project", "role"); err == nil {
		t.Fatal("accepted symlink")
	}
	parent := t.TempDir()
	link := filepath.Join(parent, "linked-root")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skip(err)
	}
	resolved, err := workservice.ResolveMemberWorkspaceRoot(link)
	if err != nil {
		t.Fatal(err)
	}
	throughAlias, err := prepareMemberWorkspace(link, "project", "role")
	if err != nil {
		t.Fatal(err)
	}
	direct, err := prepareMemberWorkspace(resolved, "project", "role")
	if err != nil || throughAlias != direct {
		t.Fatalf("configured root alias split member storage: %q != %q: %v", throughAlias, direct, err)
	}
}

func TestMemberWorkspaceLaunchAcrossTasksAndCWD(t *testing.T) {
	var snapshots []agentproto.RuntimeLaunchSnapshot
	h, store := hostFixture(t, func(options relayruntime.HeadlessLaunchOptions) (int, error) {
		for _, entry := range options.Env {
			key, value, _ := strings.Cut(entry, "=")
			if key != config.WorkLaunchSnapshotEnv {
				continue
			}
			data, err := os.ReadFile(value)
			if err != nil {
				return 0, err
			}
			var snapshot agentproto.RuntimeLaunchSnapshot
			if err := json.Unmarshal(data, &snapshot); err != nil {
				return 0, err
			}
			snapshots = append(snapshots, snapshot)
		}
		return 1234 + len(snapshots), nil
	})
	err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		if err := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "role", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: "role", Active: true, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "project-scope", Kind: work.ScopeProject, ProjectID: "project", TeamID: "team", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProject(work.Project{ID: "project", ScopeID: "project-scope", TeamID: "team", Name: "Project", LeaderRoleID: "role", State: work.ProjectActive, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: "role", State: work.ProjectMemberReady, BotGeneration: 1, Revision: 1}, 0); err != nil {
			return err
		}
		for _, id := range []string{"task-one", "task-two"} {
			if err := tx.PutTask(work.Task{ID: id, ScopeID: "project-scope", Title: id, OwnerRoleID: "role", State: work.TaskTodo, RequirementsRevision: 1, AssignmentGeneration: 1, Revision: 1}, 0); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cwd1, cwd2 := t.TempDir(), t.TempDir()
	h.config.Settings = testSettings{location: work.ExecutionLocation{ID: "location-one", HostID: "local", Cwd: cwd1, Generation: 1, Revision: 1}}
	if err := h.Prepare(t.Context(), workengine.WorkerNeed{ScopeID: "project-scope", RoleID: "role", TaskID: "task-one"}); err != nil {
		t.Fatal(err)
	}
	// Simulate enabling member workspaces after an existing carrier was frozen.
	if err := h.service.WithMemberWorkspaceHost(h.config.Paths.StateDir, "local"); err != nil {
		t.Fatal(err)
	}
	var contextID string
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		workers, err := tx.ListWorkers("project-scope")
		if err != nil {
			return err
		}
		contextID = workers[0].ContextID
		return tx.PutInbox(work.Inbox{ID: "plan-input", ScopeID: "project-scope", RecipientRoleID: "role", RecipientContextID: contextID, TaskID: "task-one", EventID: "plan-event", State: work.InboxPending, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	candidate := readyLifecycleCandidate(t, store, contextID)
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		task, err := tx.GetTask("task-one")
		if err != nil {
			return err
		}
		candidate.Task = &task
		input, err := tx.GetInbox("plan-input")
		candidate.Inputs = []work.Inbox{input}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	plan, err := h.Plan(t.Context(), candidate)
	if err != nil {
		t.Fatalf("existing carrier must remain reusable: %v", err)
	}
	workspaceRoot, err := workservice.ResolveMemberWorkspaceRoot(h.config.Paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	workspacePath, _ := workservice.MemberWorkspacePath(workspaceRoot, "project", "role")
	found := false
	for _, input := range plan.Command.Prompt.Inputs {
		if strings.Contains(input.Text, workspacePath) && strings.Contains(input.Text, "member.workspace.inspect") {
			found = true
		}
	}
	if !found {
		t.Fatal("per-turn workspace entry missing")
	}
	h.config.Settings = testSettings{location: work.ExecutionLocation{ID: "location-two", HostID: "local", Cwd: cwd2, Generation: 1, Revision: 1}}
	if err := h.Prepare(t.Context(), workengine.WorkerNeed{ScopeID: "project-scope", RoleID: "role", TaskID: "task-two"}); err != nil {
		t.Fatal(err)
	}
	path, err := workservice.MemberWorkspacePath(workspaceRoot, "project", "role")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].CWD != cwd1 || snapshots[1].CWD != cwd2 || strings.Contains(snapshots[0].InstructionBundle.Text, path) || strings.Contains(snapshots[1].InstructionBundle.Text, path) {
		t.Fatalf("wrong launch snapshots: %+v", snapshots)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("workspace not created: %v", err)
	}
}
