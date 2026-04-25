package issueworkflow

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestClosePlanBlocksWithoutVerifierResult(t *testing.T) {
	issue := Issue{
		Number: 22,
		Title:  "完善 issue close gate",
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 建议范围",
			"stage 1",
			"## 实现参考",
			"impl",
			"## 检查参考",
			"check",
			"## 收尾参考",
			"finish",
			"## 执行决策",
			"- 是否拆分：否",
			"- 当前执行单元：#22",
			"- verifier 决策：需要",
			"## 执行快照",
			"- 当前阶段：阶段 2",
			"- 当前执行点：close-out",
			"- 已完成：close gate implementation",
			"- 下一步：run verifier",
			"- 恢复步骤：rerun verifier",
		}, "\n"),
		Labels: []string{"maintainability", "area:daemon", statusLabelImplementable},
	}
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			originURL: "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: &fakeGitHubClient{
			issues: map[int]Issue{22: issue},
		},
		Now: time.Now,
	}
	result, err := svc.ClosePlan(context.Background(), ClosePlanOptions{IssueNumber: 22, WorkflowMode: WorkflowModeFull})
	if err != nil {
		t.Fatalf("ClosePlan error = %v", err)
	}
	if result.CloseReady {
		t.Fatalf("expected close plan to be blocked, got %#v", result)
	}
	check := findNamedCheck(result.Checks, "issue_close_verifier_gate")
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("verifier gate = %#v, want fail", check)
	}
	if !hasClosePlanAction(result.NextActions, "record_verifier_result") {
		t.Fatalf("expected record_verifier_result action, got %#v", result.NextActions)
	}
	if !hasClosePlanAction(result.NextActions, "rerun_close_plan") {
		t.Fatalf("expected rerun_close_plan action, got %#v", result.NextActions)
	}
}

func TestClosePlanBlocksWithoutParentRollup(t *testing.T) {
	child := Issue{
		Number: 248,
		Title:  "子 issue",
		Body: strings.Join([]string{
			"## 父 issue",
			"- #247",
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 执行决策",
			"- 是否拆分：否",
			"- 当前执行单元：#248",
			"- verifier 决策：用户显式豁免 verifier",
			"## 实现参考",
			"impl",
			"## 检查参考",
			"check",
			"## 收尾参考",
			"finish",
			"## 执行快照",
			"- 当前阶段：收尾",
			"- 当前执行点：回卷后关闭",
			"- 已完成：implementation",
			"- 下一步：parent roll-up",
			"- 恢复步骤：update parent",
			"独立 verifier 结果：pass",
		}, "\n"),
		Labels: []string{"maintainability", "area:daemon", statusLabelImplementable},
	}
	parent := Issue{
		Number: 247,
		Title:  "母 issue",
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
		}, "\n"),
	}
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			originURL: "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: &fakeGitHubClient{
			issues: map[int]Issue{
				247: parent,
				248: child,
			},
		},
		Now: time.Now,
	}
	result, err := svc.ClosePlan(context.Background(), ClosePlanOptions{IssueNumber: 248, WorkflowMode: WorkflowModeFull})
	if err != nil {
		t.Fatalf("ClosePlan error = %v", err)
	}
	if result.CloseReady {
		t.Fatalf("expected close plan to be blocked, got %#v", result)
	}
	check := findNamedCheck(result.Checks, "issue_close_rollup_gate")
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("roll-up gate = %#v, want fail", check)
	}
	if !hasClosePlanAction(result.NextActions, "update_parent_rollup") {
		t.Fatalf("expected update_parent_rollup action, got %#v", result.NextActions)
	}
}

func TestClosePlanReadyWhenAllCloseGatesPass(t *testing.T) {
	issue := Issue{
		Number: 22,
		Title:  "issue workflow close gate ready",
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 建议范围",
			"stage 1",
			"## 实现参考",
			"impl",
			"## 检查参考",
			"check",
			"## 收尾参考",
			"finish",
			"## 执行决策",
			"- 是否拆分：否",
			"- 当前执行单元：#22",
			"- verifier 决策：需要",
			"## 执行快照",
			"- 当前阶段：收尾",
			"- 当前执行点：close plan",
			"- 已完成：implementation and validation",
			"- 下一步：finish --close",
			"- 恢复步骤：run finish",
			"独立 verifier 结果：pass",
		}, "\n"),
		Labels: []string{"maintainability", "area:daemon", statusLabelImplementable},
	}
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			originURL: "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: &fakeGitHubClient{
			issues: map[int]Issue{22: issue},
		},
		Now: time.Now,
	}
	result, err := svc.ClosePlan(context.Background(), ClosePlanOptions{IssueNumber: 22, WorkflowMode: WorkflowModeFull})
	if err != nil {
		t.Fatalf("ClosePlan error = %v", err)
	}
	if !result.CloseReady {
		t.Fatalf("expected close plan to be ready, got %#v", result)
	}
	check := findNamedCheck(result.Checks, "issue_close_verifier_gate")
	if check == nil || check.Status != CheckStatusPass {
		t.Fatalf("verifier gate = %#v, want pass", check)
	}
	if !hasClosePlanAction(result.NextActions, "finish_close") {
		t.Fatalf("expected finish_close action, got %#v", result.NextActions)
	}
}

func hasClosePlanAction(actions []ClosePlanAction, code string) bool {
	for _, action := range actions {
		if action.Code == code {
			return true
		}
	}
	return false
}
