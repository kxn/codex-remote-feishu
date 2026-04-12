package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/issueworkflow"
)

func main() {
	code, err := run(context.Background(), os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "issue-workflow: %v\n", err)
		os.Exit(code)
	}
}

func run(ctx context.Context, args []string) (int, error) {
	if len(args) == 0 {
		return 2, usageError("missing command")
	}
	rootDir, err := os.Getwd()
	if err != nil {
		return 1, err
	}
	svc := issueworkflow.NewService(rootDir)
	switch args[0] {
	case "prepare":
		return runPrepare(ctx, svc, args[1:])
	case "lint":
		return runLint(ctx, svc, args[1:])
	case "finish":
		return runFinish(ctx, svc, args[1:])
	default:
		return 2, usageError("unknown command %q", args[0])
	}
}

func runPrepare(ctx context.Context, svc *issueworkflow.Service, args []string) (int, error) {
	fs := flag.NewFlagSet("prepare", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoValue := fs.String("repo", "", "GitHub repo in owner/name form; defaults to origin remote")
	issueNumber := fs.Int("issue", 0, "issue number")
	comments := fs.Int("comments", 8, "recent comments to include in the snapshot")
	claim := fs.Bool("claim-processing", true, "claim the processing label when available")
	snapshotPath := fs.String("snapshot-file", "", "where to write the prepare snapshot JSON")
	modeValue := fs.String("mode", "full", "workflow mode: full or fast")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, err
	}
	mode, err := parseWorkflowMode(*modeValue)
	if err != nil {
		return 2, err
	}
	repo, err := parseOptionalRepo(*repoValue)
	if err != nil {
		return 2, err
	}
	result, err := svc.Prepare(ctx, issueworkflow.PrepareOptions{
		Repo:            repo,
		IssueNumber:     *issueNumber,
		CommentsLimit:   *comments,
		ClaimProcessing: *claim,
		SnapshotPath:    strings.TrimSpace(*snapshotPath),
		WorkflowMode:    mode,
	})
	if err != nil {
		return 1, err
	}
	if err := writeOutput(os.Stdout, result, *format, renderPrepare); err != nil {
		return 1, err
	}
	switch result.Status {
	case issueworkflow.PrepareStatusReady:
		return 0, nil
	default:
		return 3, nil
	}
}

func runLint(ctx context.Context, svc *issueworkflow.Service, args []string) (int, error) {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoValue := fs.String("repo", "", "GitHub repo in owner/name form; defaults to origin remote")
	issueNumber := fs.Int("issue", 0, "issue number")
	comments := fs.Int("comments", 8, "recent comments to inspect")
	modeValue := fs.String("mode", "full", "workflow mode: full or fast")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, err
	}
	mode, err := parseWorkflowMode(*modeValue)
	if err != nil {
		return 2, err
	}
	repo, err := parseOptionalRepo(*repoValue)
	if err != nil {
		return 2, err
	}
	result, err := svc.Lint(ctx, issueworkflow.LintOptions{
		Repo:          repo,
		IssueNumber:   *issueNumber,
		CommentsLimit: *comments,
		WorkflowMode:  mode,
	})
	if err != nil {
		return 1, err
	}
	if err := writeOutput(os.Stdout, result, *format, renderLint); err != nil {
		return 1, err
	}
	if lintHasErrors(result.Lint) {
		return 3, nil
	}
	return 0, nil
}

func runFinish(ctx context.Context, svc *issueworkflow.Service, args []string) (int, error) {
	fs := flag.NewFlagSet("finish", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoValue := fs.String("repo", "", "GitHub repo in owner/name form; defaults to origin remote")
	issueNumber := fs.Int("issue", 0, "issue number")
	commentFile := fs.String("comment-file", "", "post this file as an issue comment before cleanup")
	closeIssue := fs.Bool("close", false, "close the issue after posting the comment")
	releaseProcessing := fs.Bool("release-processing", true, "remove the processing label before finishing")
	skipChecks := fs.Bool("skip-checks", false, "skip local mechanical checks and only do GitHub cleanup")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, err
	}
	repo, err := parseOptionalRepo(*repoValue)
	if err != nil {
		return 2, err
	}
	commentPath := strings.TrimSpace(*commentFile)
	if commentPath != "" && !filepath.IsAbs(commentPath) {
		commentPath = filepath.Clean(commentPath)
	}
	result, err := svc.Finish(ctx, issueworkflow.FinishOptions{
		Repo:              repo,
		IssueNumber:       *issueNumber,
		CommentFile:       commentPath,
		CloseIssue:        *closeIssue,
		ReleaseProcessing: *releaseProcessing,
		SkipChecks:        *skipChecks,
	})
	if err != nil {
		return 1, err
	}
	if err := writeOutput(os.Stdout, result, *format, renderFinish); err != nil {
		return 1, err
	}
	if finishHasFailures(result.Checks) {
		return 3, nil
	}
	return 0, nil
}

func parseOptionalRepo(value string) (issueworkflow.Repo, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return issueworkflow.Repo{}, nil
	}
	return issueworkflow.ParseRepo(value)
}

func parseWorkflowMode(value string) (issueworkflow.WorkflowMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "full":
		return issueworkflow.WorkflowModeFull, nil
	case "fast":
		return issueworkflow.WorkflowModeFast, nil
	default:
		return "", usageError("unsupported workflow mode %q", value)
	}
}

func writeOutput[T any](out *os.File, value T, format string, render func(T) string) error {
	switch format {
	case "json":
		payload, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(payload))
		return err
	case "text":
		_, err := fmt.Fprintln(out, render(value))
		return err
	default:
		return errors.New("unsupported format " + format)
	}
}

func renderPrepare(result issueworkflow.PrepareResult) string {
	lines := []string{
		fmt.Sprintf("status: %s", result.Status),
		fmt.Sprintf("repo: %s", result.Repo),
		fmt.Sprintf("issue: #%d", result.IssueNumber),
	}
	if result.Branch != "" {
		lines = append(lines, fmt.Sprintf("branch: %s", result.Branch))
	}
	if result.Head != "" {
		lines = append(lines, fmt.Sprintf("head: %s", result.Head))
	}
	if len(result.DirtyTrackedFiles) > 0 {
		lines = append(lines, "dirty tracked files: "+strings.Join(result.DirtyTrackedFiles, ", "))
	}
	if result.Issue != nil {
		lines = append(lines, fmt.Sprintf("title: %s", result.Issue.Title))
		lines = append(lines, fmt.Sprintf("recorded state: %s", result.Lint.CurrentRecordedState))
	}
	if result.ProcessingAction != "" && result.ProcessingAction != issueworkflow.ProcessingActionNone {
		lines = append(lines, fmt.Sprintf("processing: %s", result.ProcessingAction))
	}
	if result.SnapshotPath != "" {
		lines = append(lines, fmt.Sprintf("snapshot: %s", result.SnapshotPath))
	}
	lines = append(lines, renderLintSummary(result.Lint)...)
	return strings.Join(lines, "\n")
}

func renderLint(result issueworkflow.LintResult) string {
	lines := []string{
		fmt.Sprintf("repo: %s", result.Repo),
		fmt.Sprintf("issue: #%d", result.IssueNumber),
	}
	if result.Issue != nil {
		lines = append(lines, fmt.Sprintf("title: %s", result.Issue.Title))
	}
	lines = append(lines, renderLintSummary(result.Lint)...)
	return strings.Join(lines, "\n")
}

func renderLintSummary(report issueworkflow.LintReport) []string {
	lines := []string{
		fmt.Sprintf("workflow mode: %s", report.WorkflowMode),
		fmt.Sprintf("recorded state: %s", report.CurrentRecordedState),
	}
	if len(report.RequiredMissing) > 0 {
		lines = append(lines, "missing required: "+strings.Join(report.RequiredMissing, ", "))
	}
	if len(report.PreferredMissing) > 0 {
		lines = append(lines, "missing preferred: "+strings.Join(report.PreferredMissing, ", "))
	}
	if len(report.StatusLabels) > 0 {
		lines = append(lines, "status labels: "+strings.Join(report.StatusLabels, ", "))
	}
	if len(report.CategoryLabels) > 0 {
		lines = append(lines, "category labels: "+strings.Join(report.CategoryLabels, ", "))
	}
	if len(report.ScopeLabels) > 0 {
		lines = append(lines, "scope labels: "+strings.Join(report.ScopeLabels, ", "))
	}
	if len(report.Findings) > 0 {
		lines = append(lines, "findings:")
		for _, finding := range report.Findings {
			lines = append(lines, fmt.Sprintf("  - [%s] %s", finding.Severity, finding.Message))
		}
	}
	return lines
}

func renderFinish(result issueworkflow.FinishResult) string {
	lines := []string{
		fmt.Sprintf("repo: %s", result.Repo),
		fmt.Sprintf("issue: #%d", result.IssueNumber),
	}
	if len(result.ChangedFiles) > 0 {
		lines = append(lines, "changed files: "+strings.Join(result.ChangedFiles, ", "))
	}
	if len(result.Checks) > 0 {
		lines = append(lines, "checks:")
		for _, check := range result.Checks {
			lines = append(lines, fmt.Sprintf("  - [%s] %s: %s", check.Status, check.Name, check.Message))
		}
	}
	if result.CommentPosted {
		lines = append(lines, "comment: posted")
	}
	if result.IssueClosed {
		lines = append(lines, "issue: closed")
	}
	if result.ProcessingReleased {
		lines = append(lines, "processing: released")
	}
	return strings.Join(lines, "\n")
}

func lintHasErrors(report issueworkflow.LintReport) bool {
	for _, finding := range report.Findings {
		if finding.Severity == issueworkflow.LintSeverityError {
			return true
		}
	}
	return false
}

func finishHasFailures(checks []issueworkflow.CheckResult) bool {
	for _, check := range checks {
		if check.Status == issueworkflow.CheckStatusFail {
			return true
		}
	}
	return false
}

func usageError(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s\nusage:\n  go run ./cmd/issue-workflow prepare --issue 123 [--repo owner/name] [--format text|json]\n  go run ./cmd/issue-workflow lint --issue 123 [--repo owner/name] [--format text|json]\n  go run ./cmd/issue-workflow finish --issue 123 [--comment-file path] [--close] [--skip-checks]", msg)
}
