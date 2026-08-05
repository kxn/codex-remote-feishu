package issueworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultCommentsLimit     = 8
	closeGateCommentsLimit   = 100
	processingLabel          = "processing"
	statusLabelImplementable = "status:implementable-now"
	statusLabelInvestigation = "status:needs-investigation"
	statusLabelNeedsPlan     = "status:needs-plan"
	statusLabelClarification = "status:needs-clarification"
	statusLabelBlocked       = "status:blocked"
	recordedStateMissing     = "missing-status-label"
	recordedStateMultiStatus = "invalid-multiple-status-labels"
)

var (
	requiredSections               = []string{"背景", "目标", "完成标准"}
	preferredSections              = []string{"范围", "非目标", "相关文档", "涉及文件", "建议范围", "实现参考", "检查参考", "收尾参考"}
	executionSections              = []string{"实现参考", "检查参考", "收尾参考"}
	statusLabels                   = []string{statusLabelImplementable, statusLabelInvestigation, statusLabelNeedsPlan, statusLabelClarification, statusLabelBlocked}
	categoryLabels                 = []string{"enhancement", "bug", "maintainability", "testing", "documentation"}
	executionDecisionRequiredItems = []string{"是否拆分", "当前执行单元", "verifier 决策"}
	executionSnapshotFields        = []string{"当前阶段", "当前执行点", "已完成", "下一步", "恢复步骤"}
)

type documentSections struct {
	Present map[string]bool
	Bodies  map[string]string
}

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
	result.Lint = BuildLintReport(issue, opts.WorkflowMode)
	if opts.ClaimProcessing {
		if hasLabel(issue.Labels, processingLabel) {
			if canReclaimStaleProcessing(s.Now(), issue.UpdatedAt, opts.StaleProcessingAfter, opts.ReclaimStaleProcessing) {
				if err := s.GitHub.RemoveLabels(ctx, repo, opts.IssueNumber, []string{processingLabel}); err != nil {
					return result, err
				}
				if err := s.GitHub.AddLabels(ctx, repo, opts.IssueNumber, []string{processingLabel}); err != nil {
					return result, err
				}
				result.ProcessingAction = ProcessingActionReclaimedStale
			} else {
				result.Status = PrepareStatusBlockedProcessingClaim
			}
		} else {
			if err := s.GitHub.AddLabels(ctx, repo, opts.IssueNumber, []string{processingLabel}); err != nil {
				return result, err
			}
			result.ProcessingAction = ProcessingActionClaimed
		}
		if result.Status == PrepareStatusReady {
			result.Issue.Labels = appendSortedUnique(result.Issue.Labels, processingLabel)
			result.Lint = BuildLintReport(*result.Issue, opts.WorkflowMode)
		}
	}
	if result.Status == PrepareStatusReady {
		if workflowCheck := workflowContractCheck(result.Lint); workflowCheck != nil && workflowCheck.Status == CheckStatusFail {
			result.Status = PrepareStatusBlockedWorkflowContract
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
	result.Lint = BuildLintReport(issue, opts.WorkflowMode)
	return result, nil
}

func (s *Service) Finish(ctx context.Context, opts FinishOptions) (result FinishResult, err error) {
	result = FinishResult{IssueNumber: opts.IssueNumber}
	if opts.IssueNumber <= 0 {
		return result, fmt.Errorf("finish requires a positive issue number")
	}
	repo, err := s.resolveRepo(ctx, opts.Repo)
	if err != nil {
		return result, err
	}
	result.Repo = repo.String()
	defer func() {
		released, releaseErr := s.releaseProcessingLabel(ctx, repo, opts.IssueNumber, opts.ReleaseProcessing)
		result.ProcessingReleased = released
		if releaseErr == nil {
			return
		}
		err = errors.Join(err, releaseErr)
	}()
	commentsLimit := 1
	if opts.CloseIssue {
		commentsLimit = closeGateCommentsLimit
	}
	issue, err := s.GitHub.FetchIssue(ctx, repo, opts.IssueNumber, normalizedCommentsLimit(commentsLimit))
	if err != nil {
		return result, err
	}
	report := BuildLintReport(issue, opts.WorkflowMode)
	if workflowCheck := workflowContractCheck(report); workflowCheck != nil {
		result.Checks = append(result.Checks, *workflowCheck)
		if hasFailedCheck(result.Checks) {
			return result, nil
		}
	}
	if opts.CloseIssue {
		closeChecks, err := s.closeGateChecks(ctx, repo, issue, opts.WorkflowMode)
		if err != nil {
			return result, err
		}
		result.Checks = append(result.Checks, closeChecks...)
		if hasFailedCheck(result.Checks) {
			return result, nil
		}
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
	return result, nil
}

func (s *Service) releaseProcessingLabel(ctx context.Context, repo Repo, issueNumber int, enabled bool) (bool, error) {
	if !enabled {
		return false, nil
	}
	issue, err := s.GitHub.FetchIssue(ctx, repo, issueNumber, 1)
	if err != nil {
		return false, err
	}
	if !hasLabel(issue.Labels, processingLabel) {
		return false, nil
	}
	if err := s.GitHub.RemoveLabels(ctx, repo, issueNumber, []string{processingLabel}); err != nil {
		return false, err
	}
	return true, nil
}

func BuildLintReport(issue Issue, mode WorkflowMode) LintReport {
	report := LintReport{WorkflowMode: normalizeWorkflowMode(mode)}
	sections := scanDocumentSections(issue.Body)
	structure := analyzeIssueStructure(issue.Number, issue.Body, sections)
	for _, required := range requiredSections {
		if !sections.Present[required] {
			report.RequiredMissing = append(report.RequiredMissing, required)
		}
	}
	for _, preferred := range preferredSections {
		if !sections.Present[preferred] {
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
		report.CurrentRecordedState = recordedStateMissing
	case 1:
		report.CurrentRecordedState = report.StatusLabels[0]
	default:
		report.CurrentRecordedState = recordedStateMultiStatus
	}
	if report.WorkflowMode == WorkflowModeFast && fastModeRequiresFullWorkflow(structure, sections) {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "fast-mode-ineligible-structure",
			Message:  "parent/child, staged, or resumable issue structure requires full workflow mode",
		})
	}
	fullMode := report.WorkflowMode == WorkflowModeFull
	report.WorkflowGuardrails = detectWorkflowGuardrails(issue.Body, sections, fullMode && len(report.RequiredMissing) == 0 && len(report.StatusLabels) == 1 && report.CurrentRecordedState == statusLabelImplementable)
	if len(report.RequiredMissing) > 0 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "missing-required-sections",
			Message:  "issue body is missing required sections: " + strings.Join(report.RequiredMissing, ", "),
		})
	}
	if len(report.StatusLabels) == 0 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "missing-status-label",
			Message:  "issue has no explicit workflow status label; add exactly one of " + strings.Join(statusLabels, ", "),
		})
	}
	if len(report.StatusLabels) > 1 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "multiple-status-labels",
			Message:  "issue has multiple workflow status labels: " + strings.Join(report.StatusLabels, ", "),
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
	if structure.IsParent && len(structure.MissingParentSections) > 0 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "missing-parent-issue-sections",
			Message:  "parent issue body is missing required parent sections: " + strings.Join(structure.MissingParentSections, ", "),
		})
	}
	if structure.IsParent && len(structure.MissingParentSummaryCols) > 0 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "missing-parent-summary-columns",
			Message:  "parent issue `总调度表` is missing required columns: " + strings.Join(structure.MissingParentSummaryCols, ", "),
		})
	}
	if structure.LegacyChildParentRef {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityWarning,
			Code:     "legacy-child-parent-reference",
			Message:  "child issue still references its parent only via free text; add a dedicated `父 issue` section before the next execution or close-out",
		})
	}
	if report.WorkflowGuardrails.ExecutionDecisionRequired {
		if !report.WorkflowGuardrails.ExecutionDecisionSectionFound {
			report.Findings = append(report.Findings, LintFinding{
				Severity: LintSeverityError,
				Code:     "missing-execution-decision-section",
				Message:  "implementable issue is missing `执行决策`; direct execution cannot start without split/worker/verifier decisions",
			})
		} else if len(report.WorkflowGuardrails.ExecutionDecisionMissingItems) > 0 {
			report.Findings = append(report.Findings, LintFinding{
				Severity: LintSeverityError,
				Code:     "incomplete-execution-decision",
				Message:  "issue `执行决策` is missing required items: " + strings.Join(report.WorkflowGuardrails.ExecutionDecisionMissingItems, ", "),
			})
		}
		if report.WorkflowGuardrails.SnapshotRequired && len(report.WorkflowGuardrails.SnapshotMissingFields) > 0 {
			report.Findings = append(report.Findings, LintFinding{
				Severity: LintSeverityError,
				Code:     "missing-execution-snapshot",
				Message:  "issue requires an execution snapshot before direct execution continues: " + strings.Join(report.WorkflowGuardrails.SnapshotMissingFields, ", "),
			})
		}
	}
	if report.WorkflowGuardrails.CloseoutTailOnly {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityInfo,
			Code:     "tail-only-closeout-state",
			Message:  "execution snapshot only lists close-out tail items; resume with verifier/publish/finish instead of new implementation work",
		})
	}
	if len(report.WorkflowGuardrails.SnapshotContradictions) > 0 {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "contradictory-execution-snapshot",
			Message:  "execution snapshot is self-contradictory: " + strings.Join(report.WorkflowGuardrails.SnapshotContradictions, "; "),
		})
	}
	explicitlyImplementable := len(report.RequiredMissing) == 0 && len(report.StatusLabels) == 1 && report.CurrentRecordedState == statusLabelImplementable
	explicitlyNeedsPlan := len(report.RequiredMissing) == 0 && len(report.StatusLabels) == 1 && report.CurrentRecordedState == statusLabelNeedsPlan
	if report.WorkflowMode == WorkflowModeFast && explicitlyNeedsPlan {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "fast-mode-ineligible-state",
			Message:  "`status:needs-plan` requires full workflow mode",
		})
	}
	if fullMode && explicitlyNeedsPlan && containsSection(report.PreferredMissing, "建议范围") {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "missing-staged-plan-section",
			Message:  "issue is explicitly marked `status:needs-plan` but body does not yet include `建议范围`",
		})
	}
	if fullMode && explicitlyImplementable && containsSection(report.PreferredMissing, "建议范围") {
		report.Findings = append(report.Findings, LintFinding{
			Severity: LintSeverityError,
			Code:     "missing-staged-plan-section",
			Message:  "issue is explicitly marked `status:implementable-now` but body does not yet include `建议范围`",
		})
	}
	if fullMode && explicitlyImplementable {
		missingExecutionSections := intersectSections(report.PreferredMissing, executionSections)
		if len(missingExecutionSections) > 0 {
			report.Findings = append(report.Findings, LintFinding{
				Severity: LintSeverityError,
				Code:     "missing-execution-context-sections",
				Message:  "issue is explicitly marked `status:implementable-now` but body does not yet include execution context sections: " + strings.Join(missingExecutionSections, ", "),
			})
		}
	}
	return report
}

func normalizeWorkflowMode(mode WorkflowMode) WorkflowMode {
	switch mode {
	case WorkflowModeFast:
		return WorkflowModeFast
	default:
		return WorkflowModeFull
	}
}

func fastModeRequiresFullWorkflow(structure issueStructure, sections documentSections) bool {
	return structure.IsParent ||
		structure.ParentIssueNumber > 0 ||
		sections.Present["建议范围"] ||
		sections.Present["执行快照"] ||
		sections.Present["当前执行点"] ||
		sections.Present["恢复步骤"]
}

func scanDocumentSections(body string) documentSections {
	sections := documentSections{
		Present: map[string]bool{},
		Bodies:  map[string]string{},
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	current := ""
	buffer := make([]string, 0)
	flush := func() {
		if current == "" {
			return
		}
		sections.Bodies[current] = strings.TrimSpace(strings.Join(buffer, "\n"))
		buffer = buffer[:0]
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			if current != "" {
				buffer = append(buffer, line)
			}
			continue
		}
		flush()
		title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(title, ":"), "："))
		if title != "" {
			sections.Present[title] = true
			current = title
			continue
		}
		current = ""
	}
	flush()
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

func intersectSections(values []string, targets []string) []string {
	out := make([]string, 0)
	for _, target := range targets {
		if containsSection(values, target) {
			out = append(out, target)
		}
	}
	return out
}

func detectWorkflowGuardrails(body string, sections documentSections, implementableNow bool) WorkflowGuardrails {
	guardrails := WorkflowGuardrails{
		ImplementableNow:          implementableNow,
		ExecutionDecisionRequired: implementableNow,
	}
	if !implementableNow {
		return guardrails
	}
	decisionBody, ok := sections.Bodies["执行决策"]
	guardrails.ExecutionDecisionSectionFound = ok
	if ok {
		normalizedDecision := normalizeForContains(decisionBody)
		if !strings.Contains(normalizedDecision, normalizeForContains("是否拆分")) {
			guardrails.ExecutionDecisionMissingItems = append(guardrails.ExecutionDecisionMissingItems, executionDecisionRequiredItems[0])
		}
		if !strings.Contains(normalizedDecision, normalizeForContains("当前执行单元")) {
			guardrails.ExecutionDecisionMissingItems = append(guardrails.ExecutionDecisionMissingItems, executionDecisionRequiredItems[1])
		}
		if !containsAny(normalizedDecision,
			normalizeForContains("是否需要独立 verifier"),
			normalizeForContains("是否运行 verifier"),
			normalizeForContains("verifier"),
		) {
			guardrails.ExecutionDecisionMissingItems = append(guardrails.ExecutionDecisionMissingItems, executionDecisionRequiredItems[2])
		}
	}
	guardrails.SnapshotRequired = sections.Present["建议范围"] || sections.Present["执行快照"]
	normalizedBody := normalizeForContains(body)
	for _, field := range executionSnapshotFields {
		if strings.Contains(normalizedBody, normalizeForContains(field)) {
			guardrails.SnapshotRequired = true
			break
		}
	}
	if !guardrails.SnapshotRequired {
		return guardrails
	}
	for _, field := range executionSnapshotFields {
		if !strings.Contains(normalizedBody, normalizeForContains(field)) {
			guardrails.SnapshotMissingFields = append(guardrails.SnapshotMissingFields, field)
		}
	}
	snapshotState := analyzeExecutionSnapshot(body)
	guardrails.CloseoutTailOnly = snapshotState.CloseoutTailOnly
	guardrails.SnapshotContradictions = append(guardrails.SnapshotContradictions, snapshotState.Contradictions...)
	return guardrails
}

func workflowContractCheck(report LintReport) *CheckResult {
	enforcePlanningContract := report.CurrentRecordedState == statusLabelNeedsPlan || report.CurrentRecordedState == statusLabelImplementable
	if !enforcePlanningContract && !report.WorkflowGuardrails.ExecutionDecisionRequired {
		return nil
	}
	problems := make([]string, 0)
	if enforcePlanningContract && len(report.RequiredMissing) > 0 {
		problems = append(problems, "missing required sections: "+strings.Join(report.RequiredMissing, ", "))
	}
	if report.WorkflowMode == WorkflowModeFast {
		for _, finding := range report.Findings {
			if finding.Code == "fast-mode-ineligible-state" || finding.Code == "fast-mode-ineligible-structure" {
				problems = append(problems, finding.Message)
			}
		}
		if len(problems) == 0 {
			return &CheckResult{Name: "issue_workflow_contract", Status: CheckStatusPass, Message: "ok (fast mode)"}
		}
		return &CheckResult{
			Name:    "issue_workflow_contract",
			Status:  CheckStatusFail,
			Message: strings.Join(problems, "; "),
		}
	}
	if enforcePlanningContract && containsSection(report.PreferredMissing, "建议范围") {
		problems = append(problems, "missing `建议范围` section")
	}
	if report.CurrentRecordedState == statusLabelNeedsPlan {
		if len(problems) == 0 {
			return &CheckResult{Name: "issue_workflow_contract", Status: CheckStatusPass, Message: "ok"}
		}
		return &CheckResult{
			Name:    "issue_workflow_contract",
			Status:  CheckStatusFail,
			Message: strings.Join(problems, "; "),
		}
	}
	missingExecutionSections := intersectSections(report.PreferredMissing, executionSections)
	if len(missingExecutionSections) > 0 {
		problems = append(problems, "missing execution context sections: "+strings.Join(missingExecutionSections, ", "))
	}
	if !report.WorkflowGuardrails.ExecutionDecisionSectionFound {
		problems = append(problems, "missing `执行决策` section")
	}
	if len(report.WorkflowGuardrails.ExecutionDecisionMissingItems) > 0 {
		problems = append(problems, "missing execution decision items: "+strings.Join(report.WorkflowGuardrails.ExecutionDecisionMissingItems, ", "))
	}
	if report.WorkflowGuardrails.SnapshotRequired && len(report.WorkflowGuardrails.SnapshotMissingFields) > 0 {
		problems = append(problems, "missing execution snapshot fields: "+strings.Join(report.WorkflowGuardrails.SnapshotMissingFields, ", "))
	}
	if len(report.WorkflowGuardrails.SnapshotContradictions) > 0 {
		problems = append(problems, "execution snapshot contradictions: "+strings.Join(report.WorkflowGuardrails.SnapshotContradictions, "; "))
	}
	if len(problems) == 0 {
		return &CheckResult{Name: "issue_workflow_contract", Status: CheckStatusPass, Message: "ok"}
	}
	return &CheckResult{
		Name:    "issue_workflow_contract",
		Status:  CheckStatusFail,
		Message: strings.Join(problems, "; "),
	}
}

func normalizeForContains(value string) string {
	replacer := strings.NewReplacer("`", "", "*", "", " ", "", "\t", "", "\r", "")
	return replacer.Replace(strings.ToLower(value))
}

func containsAny(value string, targets ...string) bool {
	for _, target := range targets {
		if strings.Contains(value, target) {
			return true
		}
	}
	return false
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
