package gitmeta

// WorktreeCreateErrorText 返回 worktree 创建失败的用户可读文案。nil 或未知
// 错误码回退到通用文案。单一事实来源：此前 daemon 与 orchestrator 各有一份
// 逐字节相同的实现（gitWorkspaceWorktreeErrorText /
// targetPickerWorktreeErrorText）。
func WorktreeCreateErrorText(err *WorktreeCreateError) string {
	if err == nil {
		return "worktree 创建失败，请稍后重试。"
	}
	switch err.Code {
	case WorktreeCreateErrorGitMissing:
		return "当前机器未检测到 `git`，暂时不能创建 worktree 工作区。"
	case WorktreeCreateErrorBaseWorkspaceNotGit:
		return "当前选择的工作区不是 Git 工作区，不能从它创建 worktree。"
	case WorktreeCreateErrorInvalidBranchName:
		return "新分支名无效，请检查后重试。"
	case WorktreeCreateErrorBranchExists:
		return "这个分支已经存在，请换一个新的分支名后重试。"
	case WorktreeCreateErrorInvalidDirectoryName:
		return "本地目录名无效，请改成不含路径分隔符的普通目录名。"
	case WorktreeCreateErrorDestinationExists:
		return "目标目录已经存在，请换一个目录名或基准工作区后重试。"
	default:
		return "worktree 创建失败，请稍后重试。"
	}
}
