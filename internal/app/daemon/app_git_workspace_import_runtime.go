package daemon

import "strings"

type gitWorkspaceImportRuntime struct {
	cancelled bool
	cancel    func()
}

func gitWorkspaceImportRuntimeKey(surfaceSessionID, pickerID string) string {
	return strings.TrimSpace(surfaceSessionID) + "::" + strings.TrimSpace(pickerID)
}

func (a *App) beginGitWorkspaceImportRuntimeLocked(surfaceSessionID, pickerID string, cancel func()) string {
	key := gitWorkspaceImportRuntimeKey(surfaceSessionID, pickerID)
	if key == "::" {
		return ""
	}
	a.gitWorkspaceImports[key] = &gitWorkspaceImportRuntime{cancel: cancel}
	return key
}

func (a *App) finishGitWorkspaceImportRuntimeLocked(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	runtime := a.gitWorkspaceImports[key]
	delete(a.gitWorkspaceImports, key)
	if runtime == nil {
		return false
	}
	return runtime.cancelled
}

func (a *App) cancelGitWorkspaceImportRuntimeLocked(surfaceSessionID, pickerID string) {
	key := gitWorkspaceImportRuntimeKey(surfaceSessionID, pickerID)
	runtime := a.gitWorkspaceImports[key]
	if runtime == nil {
		return
	}
	runtime.cancelled = true
	if runtime.cancel != nil {
		runtime.cancel()
	}
}
