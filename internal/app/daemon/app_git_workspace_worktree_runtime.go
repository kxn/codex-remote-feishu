package daemon

import "strings"

type gitWorkspaceWorktreeRuntime struct {
	cancelled bool
	cancel    func()
}

func gitWorkspaceWorktreeRuntimeKey(surfaceSessionID, pickerID string) string {
	return strings.TrimSpace(surfaceSessionID) + "::" + strings.TrimSpace(pickerID)
}

func (a *App) beginGitWorkspaceWorktreeRuntimeLocked(surfaceSessionID, pickerID string, cancel func()) string {
	key := gitWorkspaceWorktreeRuntimeKey(surfaceSessionID, pickerID)
	if key == "::" {
		return ""
	}
	a.gitWorkspaceWorktrees[key] = &gitWorkspaceWorktreeRuntime{cancel: cancel}
	return key
}

func (a *App) finishGitWorkspaceWorktreeRuntimeLocked(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	runtime := a.gitWorkspaceWorktrees[key]
	delete(a.gitWorkspaceWorktrees, key)
	if runtime == nil {
		return false
	}
	return runtime.cancelled
}

func (a *App) cancelGitWorkspaceWorktreeRuntimeLocked(surfaceSessionID, pickerID string) {
	key := gitWorkspaceWorktreeRuntimeKey(surfaceSessionID, pickerID)
	runtime := a.gitWorkspaceWorktrees[key]
	if runtime == nil {
		return
	}
	runtime.cancelled = true
	if runtime.cancel != nil {
		runtime.cancel()
	}
}
