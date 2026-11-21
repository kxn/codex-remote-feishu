// Package worklocation verifies locations prepared by an agent. It never
// creates worktrees, changes branches, cleans a directory or chooses a repo.
package worklocation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

type Verification struct {
	Location           work.ExecutionLocation `json:"location"`
	RootLocationID     string                 `json:"root_location_id"`
	CanonicalRoot      string                 `json:"canonical_root"`
	CommonGitDirectory string                 `json:"common_git_directory,omitempty"`
	BranchRef          string                 `json:"branch_ref,omitempty"`
	WorktreeListing    string                 `json:"worktree_listing,omitempty"`
	VerifiedAt         time.Time              `json:"verified_at"`
}

func Verify(ctx context.Context, root work.ExecutionLocation, path string) (Verification, error) {
	var proof Verification
	if root.Validate() != nil || !filepath.IsAbs(root.Cwd) || !filepath.IsAbs(path) {
		return proof, fmt.Errorf("explicit absolute selected root and prepared location required")
	}
	canonicalRoot, err := canonicalDirectory(root.Cwd)
	if err != nil {
		return proof, err
	}
	canonical, err := canonicalDirectory(path)
	if err != nil {
		return proof, err
	}
	proof = Verification{RootLocationID: root.ID, CanonicalRoot: canonicalRoot, VerifiedAt: time.Now().UTC()}
	common, gitErr := gitQuery(ctx, canonicalRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	keys := []string{"directory:" + canonical}
	if gitErr == nil {
		common, err = filepath.EvalSymlinks(strings.TrimSpace(common))
		if err != nil {
			return proof, err
		}
		other, e := gitQuery(ctx, canonical, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if e != nil {
			return proof, fmt.Errorf("prepared path is not a worktree of the selected repository")
		}
		other, e = filepath.EvalSymlinks(strings.TrimSpace(other))
		if e != nil {
			return proof, e
		}
		if common != other {
			return proof, fmt.Errorf("prepared path belongs to a different repository")
		}
		listing, e := gitQuery(ctx, canonicalRoot, "worktree", "list", "--porcelain", "-z")
		if e != nil {
			return proof, e
		}
		registered := false
		for _, field := range strings.Split(listing, "\x00") {
			if strings.HasPrefix(field, "worktree ") {
				listed, e := canonicalDirectory(strings.TrimPrefix(field, "worktree "))
				if e == nil && listed == canonical {
					registered = true
				}
			}
		}
		if !registered {
			return proof, fmt.Errorf("prepared path is not registered in the selected repository")
		}
		branch, e := gitQuery(ctx, canonical, "symbolic-ref", "-q", "HEAD")
		if e == nil {
			proof.BranchRef = strings.TrimSpace(branch)
			keys = append(keys, "git-branch:"+common+":"+proof.BranchRef)
		} else {
			if _, headErr := gitQuery(ctx, canonical, "rev-parse", "--verify", "HEAD"); headErr != nil {
				return proof, fmt.Errorf("cannot identify prepared repository HEAD")
			}
		}
		proof.CommonGitDirectory, proof.WorktreeListing = common, listing
	} else {
		if hasGitMarker(canonicalRoot) {
			return proof, fmt.Errorf("selected repository could not be inspected; location was not treated as an ordinary directory")
		}
		// A selected non-Git working directory can contain agent-created
		// subdirectories, but cannot authorize an unrelated filesystem path.
		rel, e := filepath.Rel(canonicalRoot, canonical)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return proof, fmt.Errorf("prepared directory is outside the selected resource")
		}
	}
	// Recheck symlinks after the Git queries; changing the path while being
	// inspected does not establish a usable execution binding.
	if current, e := canonicalDirectory(path); e != nil || current != canonical {
		return proof, fmt.Errorf("prepared path changed during verification")
	}
	if current, e := canonicalDirectory(root.Cwd); e != nil || current != canonicalRoot {
		return proof, fmt.Errorf("selected resource changed during verification")
	}
	raw, _ := json.Marshal([]string{root.HostID, canonical})
	sum := sha256.Sum256(raw)
	proof.Location = work.ExecutionLocation{ID: "location_" + hex.EncodeToString(sum[:20]), HostID: root.HostID, Cwd: canonical, ResourceKeys: keys, Generation: 1, Revision: 1}
	return proof, nil
}

func hasGitMarker(path string) bool {
	selected := path
	for {
		marker := filepath.Join(path, ".git")
		if info, err := os.Lstat(marker); err == nil {
			if path == selected || !info.IsDir() {
				return true
			}
			// Empty ancestor .git directories are sometimes discovery barriers;
			// they do not turn every selected ordinary folder into a repository.
			if _, headErr := os.Stat(filepath.Join(marker, "HEAD")); headErr == nil || !os.IsNotExist(headErr) {
				return true
			}
		} else if !os.IsNotExist(err) {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func canonicalDirectory(path string) (string, error) {
	value, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(value)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("execution location is not a directory")
	}
	return filepath.Clean(value), nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256*1024 {
		return 0, fmt.Errorf("repository inspection exceeded its output limit")
	}
	return b.Buffer.Write(p)
}

func gitQuery(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := execlaunch.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	var out boundedOutput
	command.Stdout = &out
	// Never return unbounded stderr or credential-bearing native diagnostics.
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("Git inspection failed: %w", err)
	}
	return out.String(), nil
}
