package gitmeta

import (
	"strings"
	"testing"
)

func TestWorktreeCreateErrorText(t *testing.T) {
	if got := WorktreeCreateErrorText(nil); !strings.Contains(got, "失败") {
		t.Fatalf("WorktreeCreateErrorText(nil) = %q, want generic failure text", got)
	}
	cases := []struct {
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
	for _, tc := range cases {
		got := WorktreeCreateErrorText(&WorktreeCreateError{Code: tc.code})
		if !strings.Contains(got, tc.want) {
			t.Fatalf("WorktreeCreateErrorText(%q) = %q, want containing %q", tc.code, got, tc.want)
		}
	}
	// 未知错误码回退到通用文案。
	got := WorktreeCreateErrorText(&WorktreeCreateError{Code: WorktreeCreateErrorCreateFailed})
	want := "worktree 创建失败，请稍后重试。"
	if got != want {
		t.Fatalf("WorktreeCreateErrorText(unknown) = %q, want %q", got, want)
	}
}
