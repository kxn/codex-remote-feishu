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
	addedLabels   []string
	removedLabels []string
	commentedFile string
	closed        bool
}

func (f *fakeGitHubClient) FetchIssue(context.Context, Repo, int, int) (Issue, error) {
	return f.issue, nil
}
func (f *fakeGitHubClient) AddLabels(_ context.Context, _ Repo, _ int, labels []string) error {
	f.addedLabels = append(f.addedLabels, labels...)
	return nil
}
func (f *fakeGitHubClient) RemoveLabels(_ context.Context, _ Repo, _ int, labels []string) error {
	f.removedLabels = append(f.removedLabels, labels...)
	return nil
}
func (f *fakeGitHubClient) Comment(_ context.Context, _ Repo, _ int, bodyFile string) error {
	f.commentedFile = bodyFile
	return nil
}
func (f *fakeGitHubClient) Close(context.Context, Repo, int) error {
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
			Labels: []string{"bug", "area:daemon"},
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

func TestBuildLintReportFlagsMissingSectionsAndStatusLabels(t *testing.T) {
	report := BuildLintReport(Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 完成标准",
			"done",
		}, "\n"),
		Labels: []string{"status:blocked", "status:needs-investigation"},
	}, WorkflowModeFull)
	if !containsSection(report.RequiredMissing, "目标") {
		t.Fatalf("required missing = %#v, want 目标", report.RequiredMissing)
	}
	if report.CurrentRecordedState != "invalid-multiple-status-labels" {
		t.Fatalf("current recorded state = %q", report.CurrentRecordedState)
	}
	if len(report.Findings) < 3 {
		t.Fatalf("expected findings for missing section and label gaps, got %#v", report.Findings)
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

func TestBuildLintReportFastModeSkipsStagedPlanInfo(t *testing.T) {
	issue := Issue{
		Body: strings.Join([]string{
			"## 背景",
			"body",
			"## 目标",
			"goal",
			"## 完成标准",
			"done",
		}, "\n"),
		Labels: []string{"bug", "area:daemon"},
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
		t.Fatalf("expected full mode to keep staged-plan info finding, got %#v", fullReport.Findings)
	}
	if hasFindingCode(fastReport.Findings, "missing-staged-plan-section") {
		t.Fatalf("did not expect fast mode to require staged-plan info finding, got %#v", fastReport.Findings)
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
