package daemon

func gitWorkspaceImportRuntimeKey(surfaceSessionID, pickerID string) string {
	return pickerRuntimeKey(surfaceSessionID, pickerID)
}

func (a *App) beginGitWorkspaceImportRuntimeLocked(surfaceSessionID, pickerID string, cancel func()) string {
	return beginCancellablePickerRuntime(a.gitWorkspaceImports, surfaceSessionID, pickerID, cancel)
}

func (a *App) finishGitWorkspaceImportRuntimeLocked(key string) bool {
	return finishCancellablePickerRuntime(a.gitWorkspaceImports, key)
}

func (a *App) cancelGitWorkspaceImportRuntimeLocked(surfaceSessionID, pickerID string) {
	cancelCancellablePickerRuntime(a.gitWorkspaceImports, surfaceSessionID, pickerID)
}
