package issueworkflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeGitClient struct {
	dirtyFiles        []string
	pullErr           error
	branch            string
	head              string
	originURL         string
	changedFiles      []string
	changedDocsStatus []string
	diffCheckOutput   map[bool]string
	diffCheckErr      map[bool]error
	gofmtUnformatted  []string
	gofmtErr          error
}

func (f *fakeGitClient) TrackedDirtyFiles(context.Context) ([]string, error) {
	return append([]string(nil), f.dirtyFiles...), nil
}
func (f *fakeGitClient) PullFFOnly(context.Context) error                { return f.pullErr }
func (f *fakeGitClient) CurrentBranch(context.Context) (string, error)   { return f.branch, nil }
func (f *fakeGitClient) HeadCommit(context.Context) (string, error)      { return f.head, nil }
func (f *fakeGitClient) OriginRemoteURL(context.Context) (string, error) { return f.originURL, nil }
func (f *fakeGitClient) ChangedFilesFromHEAD(context.Context) ([]string, error) {
	return append([]string(nil), f.changedFiles...), nil
}
func (f *fakeGitClient) ChangedDocsNameStatus(context.Context) ([]string, error) {
	return append([]string(nil), f.changedDocsStatus...), nil
}
func (f *fakeGitClient) DiffCheck(_ context.Context, cached bool) (string, error) {
	return f.diffCheckOutput[cached], f.diffCheckErr[cached]
}
func (f *fakeGitClient) GofmtList(context.Context, []string) ([]string, error) {
	return append([]string(nil), f.gofmtUnformatted...), f.gofmtErr
}

type fakeGitHubClient struct {
	issue         Issue
	issues        map[int]Issue
	addedLabels   []string
	removedLabels []string
	commentedFile string
	closed        bool
	fetchErr      error
	removeErr     error
	commentErr    error
	closeErr      error
}

func (f *fakeGitHubClient) FetchIssue(_ context.Context, _ Repo, number int, _ int) (Issue, error) {
	if f.fetchErr != nil {
		return Issue{}, f.fetchErr
	}
	if len(f.issues) != 0 {
		if issue, ok := f.issues[number]; ok {
			return issue, nil
		}
	}
	return f.issue, nil
}
func (f *fakeGitHubClient) AddLabels(_ context.Context, _ Repo, _ int, labels []string) error {
	f.addedLabels = append(f.addedLabels, labels...)
	return nil
}
func (f *fakeGitHubClient) RemoveLabels(_ context.Context, _ Repo, _ int, labels []string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removedLabels = append(f.removedLabels, labels...)
	return nil
}
func (f *fakeGitHubClient) Comment(_ context.Context, _ Repo, _ int, bodyFile string) error {
	if f.commentErr != nil {
		return f.commentErr
	}
	f.commentedFile = bodyFile
	return nil
}
func (f *fakeGitHubClient) Close(context.Context, Repo, int) error {
	if f.closeErr != nil {
		return f.closeErr
	}
	f.closed = true
	return nil
}

func TestPrepareClaimsProcessingAndWritesSnapshot(t *testing.T) {
	root := t.TempDir()
	gh := &fakeGitHubClient{
		issue: Issue{
			Number: 22,
			Title:  "修复 attach 行为",
			Body: strings.Join([]string{
				"# 标题",
				"",
				"## 背景",
				"body",
				"## 目标",
				"goal",
				"## 完成标准",
				"done",
			}, "\n"),
			Labels: []string{"bug", "area:daemon", statusLabelInvestigation},
		},
	}
	svc := &Service{
		RootDir: root,
		Git: &fakeGitClient{
			branch:    "master",
			head:      "abc123",
			originURL: "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: gh,
		Now:    func() time.Time { return time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC) },
	}
	snapshot := filepath.Join(root, ".codex", "state", "issue-workflow", "issue-22.json")
	result, err := svc.Prepare(context.Background(), PrepareOptions{IssueNumber: 22, ClaimProcessing: true, SnapshotPath: snapshot, WorkflowMode: WorkflowModeFull})
	if err != nil {
		t.Fatalf("Prepare error = %v", err)
	}
	if result.Status != PrepareStatusReady || result.ProcessingAction != ProcessingActionClaimed {
		t.Fatalf("unexpected prepare result: %#v", result)
	}
	if got := gh.addedLabels; len(got) != 1 || got[0] != "processing" {
		t.Fatalf("added labels = %#v, want processing", got)
	}
	raw, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatalf("ReadFile snapshot: %v", err)
	}
	var decoded struct {
		Result PrepareResult `json:"result"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal snapshot: %v", err)
	}
	if decoded.Result.Status != PrepareStatusReady || decoded.Result.Repo != "kxn/codex-remote-feishu" {
		t.Fatalf("unexpected snapshot result: %#v", decoded.Result)
	}
}

func TestPrepareStopsOnDirtyTrackedFiles(t *testing.T) {
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			dirtyFiles: []string{"internal/core/orchestrator/service.go"},
			originURL:  "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: &fakeGitHubClient{},
		Now:    time.Now,
	}
	result, err := svc.Prepare(context.Background(), PrepareOptions{IssueNumber: 22, ClaimProcessing: true, WorkflowMode: WorkflowModeFull})
	if err != nil {
		t.Fatalf("Prepare error = %v", err)
	}
	if result.Status != PrepareStatusBlockedDirtyWorktree || len(result.DirtyTrackedFiles) != 1 {
		t.Fatalf("unexpected dirty prepare result: %#v", result)
	}
}

func TestPrepareReclaimsStaleProcessingClaim(t *testing.T) {
	root := t.TempDir()
	updatedAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	gh := &fakeGitHubClient{
		issue: Issue{
			Number:    22,
			Title:     "修复 attach 行为",
			UpdatedAt: updatedAt,
			Body: strings.Join([]string{
				"# 标题",
				"",
				"## 背景",
				"body",
				"## 目标",
				"goal",
				"## 完成标准",
				"done",
			}, "\n"),
			Labels: []string{"bug", "area:daemon", statusLabelInvestigation, "processing"},
		},
	}
	svc := &Service{
		RootDir: root,
		Git: &fakeGitClient{
			branch:    "master",
			head:      "abc123",
			originURL: "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: gh,
		Now:    func() time.Time { return updatedAt.Add(8 * time.Hour) },
	}
	result, err := svc.Prepare(context.Background(), PrepareOptions{
		IssueNumber:            22,
		ClaimProcessing:        true,
		ReclaimStaleProcessing: true,
		StaleProcessingAfter:   6 * time.Hour,
		WorkflowMode:           WorkflowModeFull,
	})
	if err != nil {
		t.Fatalf("Prepare error = %v", err)
	}
	if result.Status != PrepareStatusReady || result.ProcessingAction != ProcessingActionReclaimedStale {
		t.Fatalf("unexpected prepare result: %#v", result)
	}
	if got := gh.removedLabels; len(got) != 1 || got[0] != "processing" {
		t.Fatalf("removed labels = %#v, want processing", got)
	}
	if got := gh.addedLabels; len(got) != 1 || got[0] != "processing" {
		t.Fatalf("added labels = %#v, want processing", got)
	}
}

func TestPrepareBlocksFreshProcessingClaim(t *testing.T) {
	updatedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	gh := &fakeGitHubClient{
		issue: Issue{
			Number:    22,
			UpdatedAt: updatedAt,
			Body: strings.Join([]string{
				"## 背景",
				"body",
				"## 目标",
				"goal",
				"## 完成标准",
				"done",
			}, "\n"),
			Labels: []string{"bug", "area:daemon", statusLabelInvestigation, "processing"},
		},
	}
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			branch:    "master",
			head:      "abc123",
			originURL: "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: gh,
		Now:    func() time.Time { return updatedAt.Add(30 * time.Minute) },
	}
	result, err := svc.Prepare(context.Background(), PrepareOptions{
		IssueNumber:            22,
		ClaimProcessing:        true,
		ReclaimStaleProcessing: true,
		StaleProcessingAfter:   6 * time.Hour,
		WorkflowMode:           WorkflowModeFull,
	})
	if err != nil {
		t.Fatalf("Prepare error = %v", err)
	}
	if result.Status != PrepareStatusBlockedProcessingClaim {
		t.Fatalf("unexpected prepare result: %#v", result)
	}
	if len(gh.removedLabels) != 0 || len(gh.addedLabels) != 0 {
		t.Fatalf("did not expect label churn for fresh processing claim, got %#v / %#v", gh.removedLabels, gh.addedLabels)
	}
}

func TestPrepareBlocksWorkflowContractForImplementableIssue(t *testing.T) {
	root := t.TempDir()
	gh := &fakeGitHubClient{
		issue: Issue{
			Number: 22,
			Title:  "修复 attach 行为",
			Body: strings.Join([]string{
				"# 标题",
				"",
				"## 背景",
				"body",
				"## 目标",
				"goal",
				"## 完成标准",
				"done",
			}, "\n"),
			Labels: []string{"bug", "area:daemon", statusLabelImplementable},
		},
	}
	svc := &Service{
		RootDir: root,
		Git: &fakeGitClient{
			branch:    "master",
			head:      "abc123",
			originURL: "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: gh,
		Now:    func() time.Time { return time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC) },
	}
	result, err := svc.Prepare(context.Background(), PrepareOptions{IssueNumber: 22, ClaimProcessing: true, WorkflowMode: WorkflowModeFull})
	if err != nil {
		t.Fatalf("Prepare error = %v", err)
	}
	if result.Status != PrepareStatusBlockedWorkflowContract {
		t.Fatalf("unexpected prepare result: %#v", result)
	}
	if got := gh.addedLabels; len(got) != 1 || got[0] != "processing" {
		t.Fatalf("added labels = %#v, want processing", got)
	}
}

func TestBuildLintReportFlagsMissingSectionsAndStatusLabels(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 完成标准",
			"done",
		}, "\n"),
		Labels: []string{statusLabelBlocked, statusLabelInvestigation},
	}, WorkflowModeFull)
	if !containsSection(report.RequiredMissing, "目标") {
		t.Fatalf("required missing = %#v, want 目标", report.RequiredMissing)
	}
	if report.CurrentRecordedState != recordedStateMultiStatus {
		t.Fatalf("current recorded state = %q", report.CurrentRecordedState)
	}
	if len(report.Findings) < 3 {
		t.Fatalf("expected findings for missing section and label gaps, got %#v", report.Findings)
	}
}

func TestBuildLintReportRequiresExplicitStatusLabel(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
		}, "\n"),
		Labels: []string{"bug", "area:daemon"},
	}, WorkflowModeFull)
	if report.CurrentRecordedState != recordedStateMissing {
		t.Fatalf("current recorded state = %q, want %q", report.CurrentRecordedState, recordedStateMissing)
	}
	if !hasFindingCode(report.Findings, "missing-status-label") {
		t.Fatalf("expected missing-status-label finding, got %#v", report.Findings)
	}
	if hasFindingCode(report.Findings, "missing-staged-plan-section") {
		t.Fatalf("did not expect implementable-only finding without explicit status label, got %#v", report.Findings)
	}
}

func TestFinishRunsChecksAndReleasesProcessing(t *testing.T) {
	root := t.TempDir()
	docPath := filepath.Join(root, "docs", "general", "example.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(docPath, []byte("# Example\n\n> Type: `general`\n> Updated: `2026-04-10`\n> Summary: `ok`\n"), 0o644); err != nil {
		t.Fatalf("WriteFile doc: %v", err)
	}
	commentFile := filepath.Join(root, "comment.md")
	if err := os.WriteFile(commentFile, []byte("done"), 0o644); err != nil {
		t.Fatalf("WriteFile comment: %v", err)
	}
	gh := &fakeGitHubClient{
		issue: Issue{
			Number: 22,
			Labels: []string{"processing"},
		},
	}
	svc := &Service{
		RootDir: root,
		Git: &fakeGitClient{
			originURL:         "https://github.com/kxn/codex-remote-feishu.git",
			changedFiles:      []string{"docs/general/example.md"},
			diffCheckOutput:   map[bool]string{false: "", true: ""},
			diffCheckErr:      map[bool]error{},
			changedDocsStatus: nil,
		},
		GitHub: gh,
		Now:    time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       22,
		CommentFile:       commentFile,
		CloseIssue:        true,
		ReleaseProcessing: true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	if !result.CommentPosted || !result.IssueClosed || !result.ProcessingReleased {
		t.Fatalf("unexpected finish result: %#v", result)
	}
	if gh.commentedFile != commentFile || !gh.closed || len(gh.removedLabels) != 1 || gh.removedLabels[0] != "processing" {
		t.Fatalf("unexpected github side effects: %#v", gh)
	}
}

func TestFinishReleasesProcessingWhenCommentFails(t *testing.T) {
	root := t.TempDir()
	commentFile := filepath.Join(root, "comment.md")
	if err := os.WriteFile(commentFile, []byte("done"), 0o644); err != nil {
		t.Fatalf("WriteFile comment: %v", err)
	}
	gh := &fakeGitHubClient{
		issue: Issue{
			Number: 22,
			Labels: []string{"processing"},
		},
		commentErr: os.ErrPermission,
	}
	svc := &Service{
		RootDir: root,
		Git: &fakeGitClient{
			originURL:       "https://github.com/kxn/codex-remote-feishu.git",
			diffCheckOutput: map[bool]string{false: "", true: ""},
			diffCheckErr:    map[bool]error{},
		},
		GitHub: gh,
		Now:    time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       22,
		CommentFile:       commentFile,
		ReleaseProcessing: true,
		SkipChecks:        true,
	})
	if err == nil || !strings.Contains(err.Error(), os.ErrPermission.Error()) {
		t.Fatalf("Finish error = %v, want comment error", err)
	}
	if !result.ProcessingReleased {
		t.Fatalf("expected processing to be released on comment failure, got %#v", result)
	}
	if len(gh.removedLabels) != 1 || gh.removedLabels[0] != "processing" {
		t.Fatalf("removed labels = %#v, want processing", gh.removedLabels)
	}
}

func TestFinishReleasesProcessingWhenChecksFail(t *testing.T) {
	gh := &fakeGitHubClient{
		issue: Issue{
			Number: 22,
			Labels: []string{"processing"},
		},
	}
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			originURL:       "https://github.com/kxn/codex-remote-feishu.git",
			diffCheckOutput: map[bool]string{false: "dirty", true: ""},
			diffCheckErr:    map[bool]error{false: os.ErrInvalid},
		},
		GitHub: gh,
		Now:    time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       22,
		ReleaseProcessing: true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	if !hasFailedCheck(result.Checks) {
		t.Fatalf("expected failed checks, got %#v", result.Checks)
	}
	if !result.ProcessingReleased {
		t.Fatalf("expected processing to be released on check failure, got %#v", result)
	}
	if len(gh.removedLabels) != 1 || gh.removedLabels[0] != "processing" {
		t.Fatalf("removed labels = %#v, want processing", gh.removedLabels)
	}
}

func TestBuildLintReportRequiresStagedPlanEvenInFastMode(t *testing.T) {
	issue := Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelImplementable},
	}
	fullReport := BuildLintReport(issue, WorkflowModeFull)
	fastReport := BuildLintReport(issue, WorkflowModeFast)
	if fullReport.WorkflowMode != WorkflowModeFull {
		t.Fatalf("full workflow mode = %q", fullReport.WorkflowMode)
	}
	if fastReport.WorkflowMode != WorkflowModeFast {
		t.Fatalf("fast workflow mode = %q", fastReport.WorkflowMode)
	}
	if !hasFindingCode(fullReport.Findings, "missing-staged-plan-section") {
		t.Fatalf("expected full mode to require staged-plan finding, got %#v", fullReport.Findings)
	}
	if !hasFindingCode(fastReport.Findings, "missing-staged-plan-section") {
		t.Fatalf("expected fast mode to keep staged-plan finding, got %#v", fastReport.Findings)
	}
}

func TestBuildLintReportFlagsMissingExecutionContextSections(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 建议范围",
			"stage 1",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelImplementable},
	}, WorkflowModeFull)
	if !hasFindingCode(report.Findings, "missing-execution-context-sections") {
		t.Fatalf("expected execution-context finding, got %#v", report.Findings)
	}
}

func TestBuildLintReportRequiresStagedPlanForNeedsPlanState(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelNeedsPlan},
	}, WorkflowModeFull)
	if !hasFindingCode(report.Findings, "missing-staged-plan-section") {
		t.Fatalf("expected staged-plan finding for needs-plan state, got %#v", report.Findings)
	}
}

func TestBuildLintReportRequiresExecutionDecisionEvenInFastMode(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelImplementable},
	}, WorkflowModeFast)
	if !hasFindingCode(report.Findings, "missing-execution-decision-section") {
		t.Fatalf("expected execution decision finding in fast mode, got %#v", report.Findings)
	}
}

func TestBuildLintReportFlagsTailOnlyCloseoutState(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 执行决策",
			"- 是否拆分：否",
			"- 当前执行单元：本 issue",
			"- verifier 决策：需要",
			"## 执行快照",
			"- 当前阶段：阶段 D",
			"- 当前执行点：close-out",
			"- 已完成：实现与验证",
			"- 下一步：run verifier",
			"- 恢复步骤：1. run verifier",
			"- 未完成尾项：verifier、commit、push、finish",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelImplementable},
	}, WorkflowModeFull)
	if !report.WorkflowGuardrails.CloseoutTailOnly {
		t.Fatalf("expected close-out tail only guardrail, got %#v", report.WorkflowGuardrails)
	}
	if !hasFindingCode(report.Findings, "tail-only-closeout-state") {
		t.Fatalf("expected tail-only-closeout-state finding, got %#v", report.Findings)
	}
}

func TestBuildLintReportFlagsContradictoryExecutionSnapshot(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 执行决策",
			"- 是否拆分：否",
			"- 当前执行单元：本 issue",
			"- verifier 决策：需要",
			"## 执行快照",
			"- 当前阶段：阶段 D",
			"- 当前执行点：正在收尾前补回归测试",
			"- 已完成：quote expansion",
			"- 下一步：补回归测试，再做 contract 收口",
			"- 恢复步骤：1. 继续改代码",
			"- 未完成尾项：verifier、commit、push、finish",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelImplementable},
	}, WorkflowModeFull)
	if len(report.WorkflowGuardrails.SnapshotContradictions) == 0 {
		t.Fatalf("expected snapshot contradictions, got %#v", report.WorkflowGuardrails)
	}
	if !hasFindingCode(report.Findings, "contradictory-execution-snapshot") {
		t.Fatalf("expected contradictory-execution-snapshot finding, got %#v", report.Findings)
	}
	check := workflowContractCheck(report)
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("expected workflow contract check to fail, got %#v", check)
	}
}

func TestWorkflowContractCheckFailsWhenNeedsPlanIssueHasNoPlan(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelNeedsPlan},
	}, WorkflowModeFull)
	check := workflowContractCheck(report)
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("expected workflow contract check to fail, got %#v", check)
	}
}

func TestWorkflowContractCheckFailsWhenImplementableIssueLacksExecutionContext(t *testing.T) {
	report := BuildLintReport(Issue{
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
			"- 当前执行单元：当前 issue",
			"- verifier 决策：需要",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelImplementable},
	}, WorkflowModeFull)
	check := workflowContractCheck(report)
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("expected workflow contract check to fail, got %#v", check)
	}
}

func TestBuildLintReportFlagsIncompleteExecutionDecision(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 执行决策",
			"- 是否拆分：不拆分",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelImplementable},
	}, WorkflowModeFull)
	if !hasFindingCode(report.Findings, "incomplete-execution-decision") {
		t.Fatalf("expected incomplete execution decision finding, got %#v", report.Findings)
	}
}

func TestBuildLintReportRequiresSnapshotWhenStagedPlanExists(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 建议范围",
			"stage 1",
			"## 执行决策",
			"- 是否拆分：不拆分",
			"- 当前执行单元：当前 issue",
			"- 是否需要独立 verifier：需要，完成后再跑",
		}, "\n"),
		Labels: []string{"bug", "area:daemon", statusLabelImplementable},
	}, WorkflowModeFull)
	if !hasFindingCode(report.Findings, "missing-execution-snapshot") {
		t.Fatalf("expected snapshot finding, got %#v", report.Findings)
	}
}

func TestBuildLintReportFlagsMissingParentSummaryColumns(t *testing.T) {
	report := BuildLintReport(Issue{
		Number: 247,
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 拆分结构",
			"- #248",
			"## 推荐顺序",
			"1. #248",
			"## 可并行组",
			"- A",
			"## 当前风险",
			"- none",
			"## 总调度表",
			"| Issue | 状态 |",
			"| --- | --- |",
			"| #248 | done |",
			"## 当前执行点",
			"close",
			"## 恢复步骤",
			"resume",
			"## 执行决策",
			"- 是否拆分：是",
			"- 当前执行单元：#247",
			"- verifier 决策：需要",
		}, "\n"),
		Labels: []string{"maintainability", "area:daemon", statusLabelImplementable},
	}, WorkflowModeFull)
	if !hasFindingCode(report.Findings, "missing-parent-summary-columns") {
		t.Fatalf("expected parent summary finding, got %#v", report.Findings)
	}
}

func TestBuildLintReportWarnsOnLegacyChildParentReference(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"本 issue 由母 issue #247 跟踪。",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 执行决策",
			"- 是否拆分：否",
			"- 当前执行单元：#248",
			"- verifier 决策：需要",
		}, "\n"),
		Labels: []string{"maintainability", "area:daemon", statusLabelImplementable},
	}, WorkflowModeFull)
	if !hasFindingCode(report.Findings, "legacy-child-parent-reference") {
		t.Fatalf("expected legacy child parent warning, got %#v", report.Findings)
	}
}

func hasFindingCode(findings []LintFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func hasCheckName(checks []CheckResult, name string) bool {
	for _, check := range checks {
		if check.Name == name {
			return true
		}
	}
	return false
}

func findCheck(checks []CheckResult, name string) *CheckResult {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

func TestValidateDocMetadataRejectsWrongType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs", "general", "bad.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("# Bad\n\n> Type: `draft`\n> Updated: `2026-04-10`\n> Summary: `bad`\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := validateDocMetadata(path, "general"); err == nil {
		t.Fatal("expected validateDocMetadata to fail")
	}
}

func TestFinishWarnsWhenKnowledgeWritebackNeedsReview(t *testing.T) {
	gh := &fakeGitHubClient{
		issue: Issue{
			Number: 22,
			Labels: []string{"processing"},
		},
	}
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			originURL:    "https://github.com/kxn/codex-remote-feishu.git",
			changedFiles: []string{"internal/app/daemon/app.go"},
			diffCheckOutput: map[bool]string{
				false: "",
				true:  "",
			},
			diffCheckErr: map[bool]error{},
		},
		GitHub: gh,
		Now:    time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       22,
		ReleaseProcessing: true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	if !hasCheckName(result.Checks, "knowledge_writeback_review") {
		t.Fatalf("expected knowledge write-back warning, got %#v", result.Checks)
	}
}

func TestFinishFailsWorkflowContractForImplementableIssue(t *testing.T) {
	gh := &fakeGitHubClient{
		issue: Issue{
			Number: 22,
			Body: strings.Join([]string{
				"## 背景",
				"body",
				"## 目标",
				"goal",
				"## 完成标准",
				"done",
			}, "\n"),
			Labels: []string{"bug", "area:daemon", statusLabelImplementable, "processing"},
		},
	}
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			originURL:       "https://github.com/kxn/codex-remote-feishu.git",
			diffCheckOutput: map[bool]string{false: "", true: ""},
			diffCheckErr:    map[bool]error{},
		},
		GitHub: gh,
		Now:    time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       22,
		ReleaseProcessing: true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	check := findCheck(result.Checks, "issue_workflow_contract")
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("expected workflow contract failure, got %#v", result.Checks)
	}
	if !result.ProcessingReleased {
		t.Fatalf("expected processing release on workflow contract failure, got %#v", result)
	}
}

func TestFinishSkipChecksDoesNotRequireExecutionDecisionForBlockedIssue(t *testing.T) {
	gh := &fakeGitHubClient{
		issue: Issue{
			Number: 22,
			Body: strings.Join([]string{
				"## 背景",
				"body",
				"## 目标",
				"goal",
				"## 完成标准",
				"done",
			}, "\n"),
			Labels: []string{"bug", "area:daemon", "status:needs-investigation", "processing"},
		},
	}
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			originURL: "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: gh,
		Now:    time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       22,
		ReleaseProcessing: true,
		SkipChecks:        true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	if findCheck(result.Checks, "issue_workflow_contract") != nil {
		t.Fatalf("did not expect workflow contract check for blocked issue, got %#v", result.Checks)
	}
}

func TestFinishCloseFailsWithoutVerifierPassForMediumIssue(t *testing.T) {
	issue := Issue{
		Number: 22,
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
			"- 当前阶段：finish-ready",
			"- 当前执行点：close",
			"- 已完成：done",
			"- 下一步：close",
			"- 恢复步骤：resume",
		}, "\n"),
		Labels: []string{"maintainability", "area:daemon", statusLabelImplementable, "processing"},
	}
	gh := &fakeGitHubClient{issue: issue}
	svc := &Service{
		RootDir: t.TempDir(),
		Git: &fakeGitClient{
			originURL: "https://github.com/kxn/codex-remote-feishu.git",
		},
		GitHub: gh,
		Now:    time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       22,
		CloseIssue:        true,
		ReleaseProcessing: true,
		SkipChecks:        true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	check := findCheck(result.Checks, "issue_close_verifier_gate")
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("expected verifier close gate failure, got %#v", result.Checks)
	}
	if result.IssueClosed {
		t.Fatalf("did not expect issue to close on verifier gate failure: %#v", result)
	}
}

func TestFinishCloseFailsWhenChildRollupMissing(t *testing.T) {
	child := Issue{
		Number: 22,
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 父 issue",
			"#247",
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
			"- 当前阶段：finish-ready",
			"- 当前执行点：close",
			"- 已完成：done",
			"- 下一步：close",
			"- 恢复步骤：resume",
		}, "\n"),
		Labels: []string{"maintainability", "area:daemon", statusLabelImplementable, "processing"},
		Comments: []IssueComment{
			{Body: "独立 verifier 结果：pass"},
		},
	}
	parent := Issue{
		Number: 247,
		Body: strings.Join([]string{
			"## 背景",
			"parent",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
		}, "\n"),
	}
	gh := &fakeGitHubClient{issues: map[int]Issue{22: child, 247: parent}}
	svc := &Service{
		RootDir: t.TempDir(),
		Git:     &fakeGitClient{originURL: "https://github.com/kxn/codex-remote-feishu.git"},
		GitHub:  gh,
		Now:     time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       22,
		CloseIssue:        true,
		ReleaseProcessing: true,
		SkipChecks:        true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	check := findCheck(result.Checks, "issue_close_rollup_gate")
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("expected child roll-up gate failure, got %#v", result.Checks)
	}
}

func TestFinishCloseFailsForLegacyChildContract(t *testing.T) {
	child := Issue{
		Number: 22,
		Body: strings.Join([]string{
			"## 背景",
			"本 issue 由母 issue #247 跟踪。",
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
			"- 当前阶段：finish-ready",
			"- 当前执行点：close",
			"- 已完成：done",
			"- 下一步：close",
			"- 恢复步骤：resume",
		}, "\n"),
		Labels:   []string{"maintainability", "area:daemon", statusLabelImplementable, "processing"},
		Comments: []IssueComment{{Body: "独立 verifier 结果：pass"}},
	}
	parent := Issue{
		Number:   247,
		Comments: []IssueComment{{Body: "子 issue `#22` 已完成并关闭，结果回卷如下：..."}},
	}
	gh := &fakeGitHubClient{issues: map[int]Issue{22: child, 247: parent}}
	svc := &Service{
		RootDir: t.TempDir(),
		Git:     &fakeGitClient{originURL: "https://github.com/kxn/codex-remote-feishu.git"},
		GitHub:  gh,
		Now:     time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       22,
		CloseIssue:        true,
		ReleaseProcessing: true,
		SkipChecks:        true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	check := findCheck(result.Checks, "issue_close_child_contract_gate")
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("expected legacy child contract gate failure, got %#v", result.Checks)
	}
}

func TestFinishCloseFailsWhenParentSummaryIncomplete(t *testing.T) {
	parent := Issue{
		Number: 247,
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 拆分结构",
			"- #248",
			"## 推荐顺序",
			"1. #248",
			"## 可并行组",
			"- A",
			"## 当前风险",
			"- none",
			"## 总调度表",
			"| Issue | 状态 |",
			"| --- | --- |",
			"| #248 | done |",
			"## 当前执行点",
			"close",
			"## 恢复步骤",
			"resume",
			"## 建议范围",
			"close-out",
			"## 实现参考",
			"impl",
			"## 检查参考",
			"check",
			"## 收尾参考",
			"finish",
			"## 执行决策",
			"- 是否拆分：是",
			"- 当前执行单元：#247",
			"- verifier 决策：需要",
			"## 执行快照",
			"- 当前阶段：finish-ready",
			"- 当前执行点：close",
			"- 已完成：done",
			"- 下一步：close",
			"- 恢复步骤：resume",
		}, "\n"),
		Labels: []string{"maintainability", "area:daemon", statusLabelImplementable, "processing"},
		Comments: []IssueComment{
			{Body: "独立 verifier 结果：pass"},
			{Body: "子 issue `#248` 已完成并关闭，结果回卷如下：..."},
		},
	}
	gh := &fakeGitHubClient{issue: parent}
	svc := &Service{
		RootDir: t.TempDir(),
		Git:     &fakeGitClient{originURL: "https://github.com/kxn/codex-remote-feishu.git"},
		GitHub:  gh,
		Now:     time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       247,
		CloseIssue:        true,
		ReleaseProcessing: true,
		SkipChecks:        true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	check := findCheck(result.Checks, "issue_close_parent_summary_gate")
	if check == nil || check.Status != CheckStatusFail {
		t.Fatalf("expected parent summary gate failure, got %#v", result.Checks)
	}
}

func TestFinishClosePassesWithVerifierAndRollups(t *testing.T) {
	child := Issue{
		Number: 248,
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 父 issue",
			"#247",
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
			"- 当前执行单元：#248",
			"- verifier 决策：需要",
			"## 执行快照",
			"- 当前阶段：finish-ready",
			"- 当前执行点：close",
			"- 已完成：done",
			"- 下一步：close",
			"- 恢复步骤：resume",
		}, "\n"),
		Labels: []string{"maintainability", "area:daemon", statusLabelImplementable, "processing"},
		Comments: []IssueComment{
			{Body: "独立 verifier 结果：pass"},
		},
	}
	parent := Issue{
		Number: 247,
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
			"## 拆分结构",
			"- #248",
			"## 推荐顺序",
			"1. #248",
			"## 可并行组",
			"- A",
			"## 当前风险",
			"- none",
			"## 总调度表",
			"| Issue | 状态 | 结果回卷 | verifier 状态 | 当前结论 |",
			"| --- | --- | --- | --- | --- |",
			"| #248 | done | yes | pass | ok |",
			"## 当前执行点",
			"close",
			"## 恢复步骤",
			"resume",
			"## 建议范围",
			"close-out",
			"## 实现参考",
			"impl",
			"## 检查参考",
			"check",
			"## 收尾参考",
			"finish",
			"## 执行决策",
			"- 是否拆分：是",
			"- 当前执行单元：#247",
			"- verifier 决策：需要",
			"## 执行快照",
			"- 当前阶段：finish-ready",
			"- 当前执行点：close",
			"- 已完成：done",
			"- 下一步：close",
			"- 恢复步骤：resume",
		}, "\n"),
		Comments: []IssueComment{
			{Body: "独立 verifier 结果：pass"},
			{Body: "子 issue `#248` 已完成并关闭，结果回卷如下：..."},
		},
	}
	gh := &fakeGitHubClient{
		issues: map[int]Issue{
			247: parent,
			248: child,
		},
	}
	svc := &Service{
		RootDir: t.TempDir(),
		Git:     &fakeGitClient{originURL: "https://github.com/kxn/codex-remote-feishu.git"},
		GitHub:  gh,
		Now:     time.Now,
	}
	result, err := svc.Finish(context.Background(), FinishOptions{
		IssueNumber:       248,
		CloseIssue:        true,
		ReleaseProcessing: true,
		SkipChecks:        true,
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	if check := findCheck(result.Checks, "issue_close_verifier_gate"); check == nil || check.Status != CheckStatusPass {
		t.Fatalf("expected verifier gate pass, got %#v", result.Checks)
	}
	if check := findCheck(result.Checks, "issue_close_rollup_gate"); check == nil || check.Status != CheckStatusPass {
		t.Fatalf("expected roll-up gate pass, got %#v", result.Checks)
	}
	if !result.IssueClosed {
		t.Fatalf("expected issue to close, got %#v", result)
	}
}
