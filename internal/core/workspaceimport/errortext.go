package workspaceimport

// ErrorText 返回 Git 导入失败的用户可读文案。nil 或未知错误码回退到通用文案。
// 单一事实来源：此前 daemon 与 orchestrator 各有一份逐字节相同的实现
// （gitWorkspaceImportErrorText / targetPickerGitImportErrorText）。
func ErrorText(err *ImportError) string {
	if err == nil {
		return "Git 仓库导入失败，请稍后重试。"
	}
	switch err.Code {
	case ImportErrorGitMissing:
		return "当前机器未检测到 `git`，暂时不能直接从 Git URL 导入。"
	case ImportErrorInvalidURL:
		return "Git 仓库地址无效，请检查地址格式后重试。"
	case ImportErrorInvalidDirectoryName:
		return "目标目录名无效，请改成不含路径分隔符的普通目录名。"
	case ImportErrorDestinationExists:
		return "目标目录已经存在，请换一个父目录或目录名后重试。"
	case ImportErrorRefNotFound:
		return "指定的分支或标签不存在，请检查后重试。"
	case ImportErrorAuthFailed:
		return "无法访问这个仓库，请确认当前机器上的 Git 凭据或仓库权限后重试。"
	default:
		return "Git 仓库导入失败，请稍后重试。"
	}
}
