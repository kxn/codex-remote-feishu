package issueworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultCommentsLimit = 8
	processingLabel      = "processing"
)

var (
	requiredSections  = []string{"背景", "目标", "完成标准"}
	preferredSections = []string{"范围", "非目标", "相关文档", "涉及文件", "建议范围"}
	statusLabels      = []string{"status:needs-investigation", "status:needs-clarification", "status:blocked"}
	categoryLabels    = []string{"enhancement", "bug", "maintainability", "testing", "documentation"}
)

type Service struct {
	RootDir string
	Git     GitClient
	GitHub  GitHubClient
	Now     func() time.Time
}

func NewService(rootDir string) *Service {
	return &Service{
		RootDir: rootDir,
		Git:     NewGitCLI(rootDir),
		GitHub:  NewGitHubCLI(),
		Now:     func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Prepare(ctx context.Context, opts PrepareOptions) (PrepareResult, error) {
	result := PrepareResult{
		Status:           PrepareStatusReady,
		IssueNumber:      opts.IssueNumber,
		Repo:             opts.Repo.String(),
		SnapshotPath:     strings.TrimSpace(opts.SnapshotPath),
		ProcessingAction: ProcessingActionNone,
	}
	if opts.IssueNumber <= 0 {
		return result, fmt.Errorf("prepare requires a positive issue number")
	}
	repo, err := s.resolveRepo(ctx, opts.Repo)
	if err != nil {
		return result, err
	}
	result.Repo = repo.String()
	dirty, err := s.Git.TrackedDirtyFiles(ctx)
	if err != nil {
		return result, err
	}
	if len(dirty) > 0 {
		result.Status = PrepareStatusBlockedDirtyWorktree
		result.DirtyTrackedFiles = dirty
		return result, nil
	}
	if err := s.Git.PullFFOnly(ctx); err != nil {
		return result, err
	}
	if result.Branch, err = s.Git.CurrentBranch(ctx); err != nil {
		return result, err
	}
	if result.Head, err = s.Git.HeadCommit(ctx); err != nil {
		return result, err
	}
	issue, err := s.GitHub.FetchIssue(ctx, repo, opts.IssueNumber, normalizedCommentsLimit(opts.CommentsLimit))
	if err != nil {
		return result, err
	}
	result.Issue = &issue
	result.Lint = BuildLintReport(issue)
	if opts.ClaimProcessing {
		if hasLabel(issue.Labels, processingLabel) {
			result.Status = PrepareStatusBlockedProcessingClaim
		} else {
			if err := s.GitHub.AddLabels(ctx, repo, opts.IssueNumber, []string{processingLabel}); err != nil {
				return result, err
			}
			result.ProcessingAction = ProcessingActionClaimed
			result.Issue.Labels = appendSortedUnique(result.Issue.Labels, processingLabel)
			result.Lint = BuildLintReport(*result.Issue)
		}
	}
	if result.SnapshotPath == "" {
		result.SnapshotPath = filepath.Join(s.RootDir, ".codex", "state", "issue-workflow", fmt.Sprintf("issue-%d.json", opts.IssueNumber))
	}
	if err := writeJSON(result.SnapshotPath, struct {
		PreparedAt string        `json:"preparedAt"`
		Result     PrepareResult `json:"result"`
	}{
		PreparedAt: s.Now().Format(time.RFC3339),
		Result:     result,
	}); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) Lint(ctx context.Context, opts LintOptions) (LintResult, error) {
	result := LintResult{IssueNumber: opts.IssueNumber}
	if opts.IssueNumber <= 0 {
		return result, fmt.Errorf("lint requires a positive issue number")
	}
	repo, err := s.resolveRepo(ctx, opts.Repo)
	if err != nil {
		return result, err
	}
	result.Repo = repo.String()
	issue, err := s.GitHub.FetchIssue(ctx, repo, opts.IssueNumber, normalizedCommentsLimit(opts.CommentsLimit))
	if err != nil {
		return result, err
	}
	result.Issue = &issue
	result.Lint = BuildLintReport(issue)
	return result, nil
}

func (s *Service) Finish(ctx context.Context, opts FinishOptions) (FinishResult, error) {
	result := FinishResult{IssueNumber: opts.IssueNumber}
	if opts.IssueNumber <= 0 {
		return result, fmt.Errorf("finish requires a positive issue number")
	}
	repo, err := s.resolveRepo(ctx, opts.Repo)
	if err != nil {
		return result, err
	}
	result.Repo = repo.String()
	if !opts.SkipChecks {
		if result.ChangedFiles, err = s.Git.ChangedFilesFromHEAD(ctx); err != nil {
			return result, err
		}
		diffOutput, diffErr := s.Git.DiffCheck(ctx, false)
		result.Checks = append(result.Checks, diffCheckResult("git_diff_check", diffOutput, diffErr))
		cachedOutput, cachedErr := s.Git.DiffCheck(ctx, true)
		result.Checks = append(result.Checks, diffCheckResult("git_cached_diff_check", cachedOutput, cachedErr))
		if goCheck := s.gofmtCheck(ctx, result.ChangedFiles); goCheck != nil {
			result.Checks = append(result.Checks, *goCheck)
		}
		if docsCheck := s.docsMetadataCheck(result.ChangedFiles); docsCheck != nil {
			result.Checks = append(result.Checks, *docsCheck)
		}
		if docsIndexCheck, err := s.docsIndexCheck(ctx, result.ChangedFiles); err != nil {
			return result, err
		} else if docsIndexCheck != nil {
			result.Checks = append(result.Checks, *docsIndexCheck)
		}
		if surfaceCheck := s.remoteSurfaceDocCheck(result.ChangedFiles); surfaceCheck != nil {
			result.Checks = append(result.Checks, *surfaceCheck)
		}
		if hasFailedCheck(result.Checks) {
			return result, nil
		}
	}
	issue, err := s.GitHub.FetchIssue(ctx, repo, opts.IssueNumber, 1)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(opts.CommentFile) != "" {
		if _, err := os.Stat(opts.CommentFile); err != nil {
			return result, err
		}
		if err := s.GitHub.Comment(ctx, repo, opts.IssueNumber, opts.CommentFile); err != nil {
			return result, err
		}
		result.CommentPosted = true
	}
	if opts.CloseIssue {
		if err := s.GitHub.Close(ctx, repo, opts.IssueNumber); err != nil {
			return result, err
		}
		result.IssueClosed = true
	}
	if opts.ReleaseProcessing && hasLabel(issue.Labels, processingLabel) {
		if err := s.GitHub.RemoveLabels(ctx, repo, opts.IssueNumber, []string{processingLabel}); err != nil {
			return result, err
		}
		result.ProcessingReleased = true
	}
	return result, nil
}

func BuildLintReport(issue Issue) LintReport {
	report := LintReport{}
	sections := scanSections(issue.Body)
	for _, required := range requiredSections {
		if !sections[required] {
			report.RequiredMissing = append(report.RequiredMissing, required)
		}
	}
	for _, preferred := range preferredSections {
		if !sections[preferred] {
			report.PreferredMissing = append(report.PreferredMissing, preferred)
		}
	}
	for _, label := range issue.Labels {
		switch {
		case isStatusLabel(label):
			report.StatusLabels = append(report.StatusLabels, label)
		case isCategoryLabel(label):
			report.CategoryLabels = append(report.CategoryLabels, label)
		case strings.HasPrefix(label, "area:"):
			report.ScopeLabels = append(report.ScopeLabels, label)
		}
	}
	sort.Strings(report.StatusLabels)
	sort.Strings(report.CategoryLabels)
	sort.Strings(report.ScopeLabels)
	switch len(report.StatusLabels) {
	case 0:
		report.CurrentRecordedState = "implementable-now-label-wise"
	case 1:
		report.CurrentRecordedState = report.StatusLabels[0]
	default:
		report.CurrentRecordedState = "invalid-multiple-status-labels"
	}
	if len(report.RequiredMissing) > 0 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "missing-required-sections",
			Message:  "issue body is missing required sections: " + strings.Join(report.RequiredMissing, ", "),
		})
	}
	if len(report.StatusLabels) > 1 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "multiple-status-labels",
			Message:  "issue has multiple blocked-state labels: " + strings.Join(report.StatusLabels, ", "),
		})
	}
	if len(report.CategoryLabels) == 0 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityWarning,
			Code:     "missing-category-label",
			Message:  "issue has no known category label",
		})
	}
	if len(report.ScopeLabels) == 0 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityWarning,
			Code:     "missing-scope-label",
			Message:  "issue has no area:* scope label",
		})
	}
	if !containsSection(report.PreferredMissing, "建议范围") && len(report.RequiredMissing) == 0 {
		// no-op: explicit staged-plan section already present
	} else if len(report.RequiredMissing) == 0 && len(report.StatusLabels) == 0 && containsSection(report.PreferredMissing, "建议范围") {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityInfo,
			Code:     "missing-staged-plan-section",
			Message:  "issue is label-wise implementable but body does not yet include `建议范围`",
		})
	}
	return report
}

func scanSections(body string) map[string]bool {
	sections := map[string]bool{}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(title, ":"), "："))
		if title != "" {
			sections[title] = true
		}
	}
	return sections
}

func (s *Service) resolveRepo(ctx context.Context, repo Repo) (Repo, error) {
	if repo.String() != "" {
		return repo, nil
	}
	remoteURL, err := s.Git.OriginRemoteURL(ctx)
	if err != nil {
		return Repo{}, err
	}
	return RepoFromRemoteURL(remoteURL)
}

func normalizedCommentsLimit(limit int) int {
	if limit <= 0 {
		return defaultCommentsLimit
	}
	return limit
}

func hasLabel(labels []string, target string) bool {
	for _, label := range labels {
		if strings.TrimSpace(label) == target {
			return true
		}
	}
	return false
}

func appendSortedUnique(values []string, next string) []string {
	values = append(values, next)
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func isStatusLabel(label string) bool {
	for _, status := range statusLabels {
		if label == status {
			return true
		}
	}
	return false
}

func isCategoryLabel(label string) bool {
	for _, category := range categoryLabels {
		if label == category {
			return true
		}
	}
	return false
}

func containsSection(sections []string, target string) bool {
	for _, section := range sections {
		if section == target {
			return true
		}
	}
	return false
}

func diffCheckResult(name string, output string, err error) CheckResult {
	switch {
	case err == nil:
		return CheckResult{Name: name, Status: CheckStatusPass, Message: "ok"}
	case strings.TrimSpace(output) != "":
		return CheckResult{Name: name, Status: CheckStatusFail, Message: strings.TrimSpace(output)}
	default:
		return CheckResult{Name: name, Status: CheckStatusFail, Message: err.Error()}
	}
}

func (s *Service) gofmtCheck(ctx context.Context, changedFiles []string) *CheckResult {
	goFiles := make([]string, 0)
	for _, file := range changedFiles {
		if strings.HasSuffix(file, ".go") && (strings.HasPrefix(file, "cmd/") || strings.HasPrefix(file, "internal/") || strings.HasPrefix(file, "testkit/")) {
			goFiles = append(goFiles, file)
		}
	}
	if len(goFiles) == 0 {
		return nil
	}
	unformatted, err := s.Git.GofmtList(ctx, goFiles)
	if err != nil {
		return &CheckResult{Name: "gofmt_changed_go_files", Status: CheckStatusFail, Message: err.Error()}
	}
	if len(unformatted) == 0 {
		return &CheckResult{Name: "gofmt_changed_go_files", Status: CheckStatusPass, Message: "ok"}
	}
	return &CheckResult{
		Name:    "gofmt_changed_go_files",
		Status:  CheckStatusFail,
		Message: "run gofmt on changed Go files: " + strings.Join(unformatted, ", "),
	}
}

func (s *Service) docsMetadataCheck(changedFiles []string) *CheckResult {
	docFiles := make([]string, 0)
	for _, file := range changedFiles {
		if strings.HasPrefix(file, "docs/") && strings.HasSuffix(file, ".md") {
			docFiles = append(docFiles, file)
		}
	}
	if len(docFiles) == 0 {
		return nil
	}
	failures := make([]string, 0)
	for _, file := range docFiles {
		if err := validateDocMetadata(filepath.Join(s.RootDir, file), expectedDocType(file)); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", file, err))
		}
	}
	if len(failures) == 0 {
		return &CheckResult{Name: "docs_metadata", Status: CheckStatusPass, Message: "ok"}
	}
	return &CheckResult{Name: "docs_metadata", Status: CheckStatusFail, Message: strings.Join(failures, "; ")}
}

func validateDocMetadata(path string, expectedType string) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(payload), "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i]), "# ") {
		return fmt.Errorf("missing title line")
	}
	i++
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	required := []string{"> Type:", "> Updated:", "> Summary:"}
	values := map[string]string{}
	for _, prefix := range required {
		if i >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i]), prefix) {
			return fmt.Errorf("missing metadata line %s", prefix)
		}
		value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), prefix))
		values[prefix] = strings.Trim(value, "` ")
		i++
	}
	if expectedType != "" && values["> Type:"] != expectedType {
		return fmt.Errorf("type %q does not match docs/%s", values["> Type:"], expectedType)
	}
	if values["> Updated:"] == "" {
		return fmt.Errorf("updated metadata is empty")
	}
	if values["> Summary:"] == "" {
		return fmt.Errorf("summary metadata is empty")
	}
	return nil
}

func expectedDocType(file string) string {
	parts := strings.Split(filepath.ToSlash(file), "/")
	if len(parts) < 3 || parts[0] != "docs" {
		return ""
	}
	return parts[1]
}

func (s *Service) docsIndexCheck(ctx context.Context, changedFiles []string) (*CheckResult, error) {
	docReadmeChanged := false
	for _, file := range changedFiles {
		if file == "docs/README.md" {
			docReadmeChanged = true
			break
		}
	}
	statusLines, err := s.Git.ChangedDocsNameStatus(ctx)
	if err != nil {
		return nil, err
	}
	needsReadme := false
	for _, line := range statusLines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		switch {
		case strings.HasPrefix(status, "A"), strings.HasPrefix(status, "D"), strings.HasPrefix(status, "R"), strings.HasPrefix(status, "C"):
			needsReadme = true
		}
	}
	if !needsReadme {
		return nil, nil
	}
	if docReadmeChanged {
		return &CheckResult{Name: "docs_readme_index", Status: CheckStatusPass, Message: "ok"}, nil
	}
	return &CheckResult{
		Name:    "docs_readme_index",
		Status:  CheckStatusFail,
		Message: "docs add/delete/rename detected without updating docs/README.md",
	}, nil
}

func (s *Service) remoteSurfaceDocCheck(changedFiles []string) *CheckResult {
	sensitive := make([]string, 0)
	docTouched := false
	for _, file := range changedFiles {
		switch {
		case file == "docs/general/remote-surface-state-machine.md":
			docTouched = true
		case strings.HasSuffix(file, "_test.go"):
		case strings.HasPrefix(file, "internal/core/orchestrator/"):
			sensitive = append(sensitive, file)
		case strings.HasPrefix(file, "internal/core/control/"):
			sensitive = append(sensitive, file)
		case file == "internal/core/state/types.go":
			sensitive = append(sensitive, file)
		case file == "internal/app/daemon/app_inbound_lifecycle.go":
			sensitive = append(sensitive, file)
		}
	}
	if len(sensitive) == 0 || docTouched {
		return nil
	}
	sort.Strings(sensitive)
	return &CheckResult{
		Name:    "remote_surface_doc_guard",
		Status:  CheckStatusWarning,
		Message: "remote-surface-sensitive files changed without touching docs/general/remote-surface-state-machine.md: " + strings.Join(sensitive, ", "),
	}
}

func hasFailedCheck(checks []CheckResult) bool {
	for _, check := range checks {
		if check.Status == CheckStatusFail {
			return true
		}
	}
	return false
}

func writeJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}
