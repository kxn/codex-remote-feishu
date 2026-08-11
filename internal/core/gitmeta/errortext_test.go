package gitmeta

import (
	"strings"
	"testing"
)

func TestWorktreeCreateErrorText(t *testing.T) {
	if got := WorktreeCreateErrorText(nil); !strings.Contains(got, "失败") {
		t.Fatalf("WorktreeCreateErrorText(nil) = %q, want generic failure text", got)
	}
	businessCases := []struct {
		code WorktreeCreateErrorCode
		want string
	}{
		{WorktreeCreateErrorGitMissing, "`git`"},
		{WorktreeCreateErrorBaseWorkspaceNotGit, "不是 Git 工作区"},
		{WorktreeCreateErrorInvalidBranchName, "分支名无效"},
		{WorktreeCreateErrorBranchExists, "已经存在"},
		{WorktreeCreateErrorInvalidDirectoryName, "目录名无效"},
		{WorktreeCreateErrorDestinationExists, "已经存在"},
	}
	for _, tc := range businessCases {
		got := WorktreeCreateErrorText(&WorktreeCreateError{Code: tc.code})
		if !strings.Contains(got, tc.want) {
			t.Fatalf("WorktreeCreateErrorText(%q) = %q, want containing %q", tc.code, got, tc.want)
		}
	}
	// 非业务错误码（协议/运行失败）回退到通用文案。
	for _, code := range []WorktreeCreateErrorCode{
		WorktreeCreateErrorInvalidBaseWorkspace,
		WorktreeCreateErrorCreateFailed,
	} {
		got := WorktreeCreateErrorText(&WorktreeCreateError{Code: code})
		want := "worktree 创建失败，请稍后重试。"
		if got != want {
			t.Fatalf("WorktreeCreateErrorText(%q) = %q, want %q", code, got, want)
		}
	}
}
