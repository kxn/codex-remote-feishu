package daemon

func gitWorkspaceWorktreeRuntimeKey(surfaceSessionID, pickerID string) string {
	return pickerRuntimeKey(surfaceSessionID, pickerID)
}

func (a *App) beginGitWorkspaceWorktreeRuntimeLocked(surfaceSessionID, pickerID string, cancel func()) string {
	return beginCancellablePickerRuntime(a.gitWorkspaceWorktrees, surfaceSessionID, pickerID, cancel)
}

func (a *App) finishGitWorkspaceWorktreeRuntimeLocked(key string) bool {
	return finishCancellablePickerRuntime(a.gitWorkspaceWorktrees, key)
}

func (a *App) cancelGitWorkspaceWorktreeRuntimeLocked(surfaceSessionID, pickerID string) {
	cancelCancellablePickerRuntime(a.gitWorkspaceWorktrees, surfaceSessionID, pickerID)
}
