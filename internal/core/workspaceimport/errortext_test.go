package workspaceimport

import (
	"strings"
	"testing"
)

func TestErrorText(t *testing.T) {
	if got := ErrorText(nil); !strings.Contains(got, "失败") {
		t.Fatalf("ErrorText(nil) = %q, want generic failure text", got)
	}
	cases := []struct {
		code ImportErrorCode
		want string
	}{
		{ImportErrorGitMissing, "`git`"},
		{ImportErrorInvalidURL, "地址无效"},
		{ImportErrorInvalidDirectoryName, "目录名"},
		{ImportErrorDestinationExists, "已经存在"},
		{ImportErrorRefNotFound, "分支或标签不存在"},
		{ImportErrorAuthFailed, "凭据"},
	}
	for _, tc := range cases {
		got := ErrorText(&ImportError{Code: tc.code})
		if !strings.Contains(got, tc.want) {
			t.Fatalf("ErrorText(%q) = %q, want containing %q", tc.code, got, tc.want)
		}
	}
	// 未知错误码回退到通用文案。
	got := ErrorText(&ImportError{Code: ImportErrorCloneFailed})
	want := "Git 仓库导入失败，请稍后重试。"
	if got != want {
		t.Fatalf("ErrorText(unknown) = %q, want %q", got, want)
	}
}
