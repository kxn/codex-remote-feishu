package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// WithMemberWorkspaceHost configures the local host-owned workspace root before serving requests.
func (s *Service) WithMemberWorkspaceHost(stateDir, hostID string) error {
	root, err := ResolveMemberWorkspaceRoot(stateDir)
	if err != nil {
		return err
	}
	if hostID == "" {
		return fmt.Errorf("member workspace requires absolute state directory and host")
	}
	s.memberWorkspaceRoot, s.memberWorkspaceHost = root, hostID
	return nil
}

// ResolveMemberWorkspaceRoot freezes the configured host root's real location.
// A configured alias (including macOS /var) is legitimate; member-created path
// components below that root are separately checked by the host. This is not an
// OS security boundary against processes running with equivalent permissions.
func ResolveMemberWorkspaceRoot(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("member workspace state root must be absolute")
	}
	actual, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(actual)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("member workspace state root must be a directory")
	}
	return filepath.Clean(actual), nil
}

// MemberWorkspacePath is pure: callers resolve the trusted root before entering
// database transactions or deriving member paths.
func MemberWorkspacePath(stateDir, projectID, roleID string) (string, error) {
	if !filepath.IsAbs(stateDir) || projectID == "" || roleID == "" {
		return "", fmt.Errorf("invalid member workspace identity")
	}
	data, _ := json.Marshal([]string{projectID, roleID})
	hash := sha256.Sum256(data)
	return filepath.Join(stateDir, "team-member-workspaces", hex.EncodeToString(hash[:]), "files"), nil
}

type MemberWorkspaceItem struct {
	Key            string           `json:"key"`
	Kind           string           `json:"kind"`
	ID             string           `json:"id"`
	Revision       int64            `json:"revision,omitempty"`
	Title          string           `json:"title,omitempty"`
	State          string           `json:"state,omitempty"`
	TaskID         string           `json:"task_id,omitempty"`
	Checkpoint     string           `json:"checkpoint,omitempty"`
	Path           string           `json:"path,omitempty"`
	LocationStatus string           `json:"location_status,omitempty"`
	Sources        []work.SourceRef `json:"sources,omitempty"`
}
type MemberWorkspaceView struct {
	Path       string                `json:"path"`
	Items      []MemberWorkspaceItem `json:"items"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

func (s *Service) MemberWorkspaceInspect(ctx context.Context, token, run string, req KnowledgeListRequest) (MemberWorkspaceView, error) {
	var result MemberWorkspaceView
	candidates := map[string][]string{}
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		wc, err := tx.GetContext(c.Worker.ContextID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopeProject || wc.Purpose == work.PurposeControl || wc.Purpose == work.PurposeReview || s.memberWorkspaceRoot == "" {
			return workstore.ErrScopeDenied
		}
		result.Path, err = MemberWorkspacePath(s.memberWorkspaceRoot, scope.ProjectID, c.Worker.RoleID)
		if err != nil {
			return err
		}
		read := callerRead(c)
		memories, err := tx.ListMemory(read)
		if err != nil {
			return err
		}
		items := []MemberWorkspaceItem{}
		for _, m := range memories {
			items = append(items, MemberWorkspaceItem{Key: "memory:" + m.ID, Kind: "memory", ID: m.ID, Revision: m.Revision, Title: m.Applicability, State: string(m.State), Sources: m.Sources})
		}
		tasks, err := tx.ListTasks(scope.ID)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if task.OwnerRoleID == c.Worker.RoleID {
				items = append(items, MemberWorkspaceItem{Key: "task:" + task.ID, Kind: "task", ID: task.ID, Title: task.Title, State: string(task.State), Revision: task.Revision})
			}
		}
		contexts, err := tx.ListContexts(scope.ID)
		if err != nil {
			return err
		}
		workers, err := tx.ListWorkers(scope.ID)
		if err != nil {
			return err
		}
		for _, wc := range contexts {
			if wc.RoleID != c.Worker.RoleID || wc.Purpose == work.PurposeControl || wc.Purpose == work.PurposeReview {
				continue
			}
			item := MemberWorkspaceItem{Key: "context:" + wc.ID, Kind: "context", ID: wc.ID, TaskID: wc.TaskID, Checkpoint: wc.Checkpoint, Revision: wc.Revision, LocationStatus: "unavailable"}
			// Only an explicitly recorded isolated location proves a legacy scratch source.
			for _, worker := range workers {
				if worker.ContextID != wc.ID || worker.LocationID == "" {
					continue
				}
				location, e := tx.GetLocation(worker.LocationID)
				if e != nil {
					return e
				}
				if location.IsolatedContextID != wc.ID || location.HostID != s.memberWorkspaceHost || location.Generation != worker.LocationGeneration || !filepath.IsAbs(location.Cwd) {
					continue
				}
				candidates[item.Key] = append(candidates[item.Key], location.Cwd)
			}
			items = append(items, item)
		}
		resources, err := tx.ListResources(scope.ID)
		if err != nil {
			return err
		}
		for _, v := range resources {
			items = append(items, MemberWorkspaceItem{Key: "resource:" + v.ID, Kind: "resource", ID: v.ID, Revision: v.Revision, Title: v.Purpose})
		}
		page, err := knowledgePage(items, req, func(v MemberWorkspaceItem) string { return v.Key })
		if err != nil {
			return err
		}
		result.Items, result.NextCursor = page.Items, page.NextCursor
		return nil
	})
	if err != nil {
		return MemberWorkspaceView{}, err
	}
	// Filesystem inspection can block; it must never hold the store transaction.
	for i := range result.Items {
		for _, path := range candidates[result.Items[i].Key] {
			resolved, e := filepath.EvalSymlinks(path)
			if e != nil {
				continue
			}
			// A historical configured root may itself be an alias (e.g. /var
			// on macOS). Discovery records the old path, not an access grant or
			// a claim that today's directory contains the historical contents.
			info, e := os.Lstat(resolved)
			if e == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				result.Items[i].Path, result.Items[i].LocationStatus = path, "remembered_path_unverified"
				break
			}
		}
	}
	// Membership and run authority may have changed while inspecting paths.
	err = s.withWorker(ctx, token, run, func(_ *workstore.Tx, _ Caller) error { return nil })
	if err != nil {
		return MemberWorkspaceView{}, err
	}
	return result, nil
}
