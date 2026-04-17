package cronrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const staleRunRootTTL = 24 * time.Hour

type Manager struct {
	stateDir string

	mu          sync.Mutex
	sourceLocks map[string]*sync.Mutex
}

type MaterializeResult struct {
	SourceSpec   SourceSpec
	SourceKey    string
	SourceLabel  string
	ResolvedRef  string
	ResolvedSHA  string
	MirrorPath   string
	RunRoot      string
	RunDirectory string
}

func NewManager(stateDir string) *Manager {
	return &Manager{
		stateDir:    cleanAbs(stateDir),
		sourceLocks: map[string]*sync.Mutex{},
	}
}

func (m *Manager) Materialize(ctx context.Context, runID string, spec SourceSpec) (MaterializeResult, error) {
	if err := ensureGitBinary(); err != nil {
		return MaterializeResult{}, err
	}
	if m == nil || strings.TrimSpace(m.stateDir) == "" {
		return MaterializeResult{}, &Error{Code: ErrorCacheInitFailed, Message: "cron repo state directory is missing"}
	}
	spec.RepoURL = strings.TrimSpace(spec.RepoURL)
	spec.Ref = strings.TrimSpace(spec.Ref)
	sourceKey := spec.SourceKey()
	if sourceKey == "" {
		return MaterializeResult{}, &Error{Code: ErrorInvalidURL, Message: "git repository url is invalid", SourceInput: spec.RawInput}
	}

	cacheRoot := filepath.Join(m.stateDir, InternalRootDirName, CacheDirName)
	runsRoot := filepath.Join(m.stateDir, InternalRootDirName, RunsDirName)
	if err := ensureDir(cacheRoot); err != nil {
		return MaterializeResult{}, &Error{Code: ErrorCacheInitFailed, Message: "failed to create cron repo cache root", RepoURL: spec.RepoURL, Path: cacheRoot, Err: err}
	}
	if err := ensureDir(runsRoot); err != nil {
		return MaterializeResult{}, &Error{Code: ErrorCacheInitFailed, Message: "failed to create cron repo runs root", RepoURL: spec.RepoURL, Path: runsRoot, Err: err}
	}
	_ = m.cleanupStaleRuns(runsRoot)

	mirrorPath := filepath.Join(cacheRoot, sourceKey, "mirror.git")
	runRoot := filepath.Join(runsRoot, strings.TrimSpace(runID))
	runDir := filepath.Join(runRoot, "worktree")

	unlock := m.lockSource(sourceKey)
	defer unlock()

	if err := m.ensureMirror(ctx, spec, mirrorPath); err != nil {
		return MaterializeResult{}, err
	}
	resolvedRef, resolvedSHA, err := m.resolveRevision(ctx, spec, mirrorPath)
	if err != nil {
		return MaterializeResult{}, err
	}
	if err := os.RemoveAll(runRoot); err != nil {
		return MaterializeResult{}, &Error{Code: ErrorWorktreeCreateFailed, Message: "failed to prepare cron run directory", RepoURL: spec.RepoURL, Ref: resolvedRef, Path: runRoot, Err: err}
	}
	if err := ensureDir(runRoot); err != nil {
		return MaterializeResult{}, &Error{Code: ErrorWorktreeCreateFailed, Message: "failed to create cron run root", RepoURL: spec.RepoURL, Ref: resolvedRef, Path: runRoot, Err: err}
	}
	if err := gitRun(ctx, "", "--git-dir", mirrorPath, "worktree", "prune"); err != nil {
		// best effort; keep going
	}
	if err := gitRun(ctx, "", "--git-dir", mirrorPath, "worktree", "add", "--detach", runDir, resolvedSHA); err != nil {
		_ = os.RemoveAll(runRoot)
		return MaterializeResult{}, classifyGitError(ErrorWorktreeCreateFailed, "git worktree create failed", spec, runDir, err)
	}
	result := MaterializeResult{
		SourceSpec:   spec,
		SourceKey:    sourceKey,
		SourceLabel:  SourceSpec{RepoURL: spec.RepoURL, Ref: resolvedRef}.SourceLabel(),
		ResolvedRef:  resolvedRef,
		ResolvedSHA:  resolvedSHA,
		MirrorPath:   mirrorPath,
		RunRoot:      runRoot,
		RunDirectory: runDir,
	}
	return result, nil
}

func (m *Manager) CleanupRun(ctx context.Context, sourceKey, runRoot string) error {
	if m == nil || strings.TrimSpace(m.stateDir) == "" {
		return nil
	}
	runRoot = cleanAbs(runRoot)
	if runRoot == "" {
		return nil
	}
	worktreeDir := filepath.Join(runRoot, "worktree")
	mirrorPath := filepath.Join(m.stateDir, InternalRootDirName, CacheDirName, strings.TrimSpace(sourceKey), "mirror.git")
	var cleanupErr error
	if strings.TrimSpace(sourceKey) != "" && pathExists(mirrorPath) && pathExists(worktreeDir) {
		if err := gitRun(ctx, "", "--git-dir", mirrorPath, "worktree", "remove", "--force", worktreeDir); err != nil {
			cleanupErr = classifyGitError(ErrorCleanupFailed, "git worktree cleanup failed", SourceSpec{}, runRoot, err)
		}
		_ = gitRun(ctx, "", "--git-dir", mirrorPath, "worktree", "prune")
	}
	if err := os.RemoveAll(runRoot); err != nil && cleanupErr == nil {
		cleanupErr = &Error{Code: ErrorCleanupFailed, Message: "failed to remove cron run root", Path: runRoot, Err: err}
	}
	return cleanupErr
}

func (m *Manager) ensureMirror(ctx context.Context, spec SourceSpec, mirrorPath string) error {
	parentDir := filepath.Dir(mirrorPath)
	if err := ensureDir(parentDir); err != nil {
		return &Error{Code: ErrorCacheInitFailed, Message: "failed to create cron repo cache directory", RepoURL: spec.RepoURL, Path: parentDir, Err: err}
	}
	if !pathExists(mirrorPath) {
		if err := gitRun(ctx, "", "clone", "--mirror", "--", spec.RepoURL, mirrorPath); err != nil {
			return classifyGitError(ErrorCacheInitFailed, "git mirror clone failed", spec, mirrorPath, err)
		}
		return nil
	}
	if err := gitRun(ctx, "", "--git-dir", mirrorPath, "remote", "set-url", "origin", spec.RepoURL); err != nil {
		return classifyGitError(ErrorFetchFailed, "git remote update failed", spec, mirrorPath, err)
	}
	if err := gitRun(ctx, "", "--git-dir", mirrorPath, "fetch", "--prune", "origin"); err != nil {
		return classifyGitError(ErrorFetchFailed, "git fetch failed", spec, mirrorPath, err)
	}
	return nil
}

func (m *Manager) resolveRevision(ctx context.Context, spec SourceSpec, mirrorPath string) (string, string, error) {
	ref := strings.TrimSpace(spec.Ref)
	if ref == "" {
		defaultRef, err := resolveDefaultRef(ctx, spec, mirrorPath)
		if err != nil {
			return "", "", classifyGitError(ErrorRefNotFound, "git default ref resolution failed", spec, mirrorPath, err)
		}
		ref = defaultRef
	}
	candidates := []string{
		"refs/remotes/origin/" + ref + "^{commit}",
		"refs/tags/" + ref + "^{commit}",
		ref + "^{commit}",
	}
	for _, candidate := range candidates {
		sha, err := gitOutput(ctx, "", "--git-dir", mirrorPath, "rev-parse", "--verify", candidate)
		if err == nil && strings.TrimSpace(sha) != "" {
			return ref, strings.TrimSpace(sha), nil
		}
	}
	return "", "", &Error{
		Code:        ErrorRefNotFound,
		Message:     "git ref not found",
		SourceInput: spec.RawInput,
		RepoURL:     spec.RepoURL,
		Ref:         ref,
		Path:        mirrorPath,
	}
}

func resolveDefaultRef(ctx context.Context, spec SourceSpec, mirrorPath string) (string, error) {
	if ref, err := gitOutput(ctx, "", "--git-dir", mirrorPath, "symbolic-ref", "--short", "HEAD"); err == nil {
		ref = strings.TrimSpace(ref)
		ref = strings.TrimPrefix(ref, "refs/heads/")
		ref = strings.TrimPrefix(ref, "origin/")
		if ref != "" {
			return ref, nil
		}
	}
	output, err := gitOutput(ctx, "", "ls-remote", "--symref", "--", spec.RepoURL, "HEAD")
	if err != nil {
		return "", err
	}
	ref := parseDefaultRefFromLSRemote(output)
	if ref == "" {
		return "", fmt.Errorf("no default branch advertised for %s", strings.TrimSpace(spec.RepoURL))
	}
	return ref, nil
}

func parseDefaultRefFromLSRemote(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref: ") || !strings.HasSuffix(line, "\tHEAD") {
			continue
		}
		ref := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "ref: "), "\tHEAD"))
		ref = strings.TrimPrefix(ref, "refs/heads/")
		if ref != "" {
			return ref
		}
	}
	return ""
}

func (m *Manager) lockSource(sourceKey string) func() {
	m.mu.Lock()
	lock := m.sourceLocks[sourceKey]
	if lock == nil {
		lock = &sync.Mutex{}
		m.sourceLocks[sourceKey] = lock
	}
	m.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (m *Manager) cleanupStaleRuns(runsRoot string) error {
	entries, err := os.ReadDir(runsRoot)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-staleRunRootTTL)
	for _, entry := range entries {
		if entry == nil || !entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(runsRoot, entry.Name())
		info, err := entry.Info()
		if err != nil || info == nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(fullPath)
	}
	return nil
}
