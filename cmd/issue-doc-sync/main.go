package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/issuedocsync"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "issue-doc-sync: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("missing command")
	}
	switch args[0] {
	case "plan":
		return runPlan(ctx, args[1:])
	case "next":
		return runNext(ctx, args[1:])
	case "inspect":
		return runInspect(ctx, args[1:])
	case "record":
		return runRecord(ctx, args[1:])
	default:
		return usageError("unknown command %q", args[0])
	}
}

func runPlan(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoValue := fs.String("repo", "", "GitHub repo in owner/name form")
	statePath := fs.String("state-file", ".codex/state/issue-doc-sync/state.json", "tracked sync state file")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repoValue == "" {
		return usageError("plan requires --repo")
	}

	repo, err := issuedocsync.ParseRepo(*repoValue)
	if err != nil {
		return err
	}
	client := issuedocsync.NewGitHubCLI()
	summaries, err := client.ListClosedIssueSummaries(ctx, repo)
	if err != nil {
		return err
	}
	state, err := issuedocsync.LoadState(*statePath, repo.String())
	if err != nil {
		return err
	}
	report := issuedocsync.BuildPlanReport(repo.String(), summaries, state)
	return issuedocsync.WritePlanReport(os.Stdout, report, *format)
}

func runNext(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("next", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoValue := fs.String("repo", "", "GitHub repo in owner/name form")
	statePath := fs.String("state-file", ".codex/state/issue-doc-sync/state.json", "tracked sync state file")
	format := fs.String("format", "number", "output format: number, text, or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repoValue == "" {
		return usageError("next requires --repo")
	}

	repo, err := issuedocsync.ParseRepo(*repoValue)
	if err != nil {
		return err
	}
	client := issuedocsync.NewGitHubCLI()
	summaries, err := client.ListClosedIssueSummaries(ctx, repo)
	if err != nil {
		return err
	}
	state, err := issuedocsync.LoadState(*statePath, repo.String())
	if err != nil {
		return err
	}
	report := issuedocsync.BuildPlanReport(repo.String(), summaries, state)
	if len(report.Candidates) == 0 {
		return errors.New("no changed closed issues need review")
	}
	candidate := report.Candidates[0]
	switch *format {
	case "number":
		_, err = fmt.Fprintln(os.Stdout, candidate.Number)
		return err
	case "text":
		if _, err := fmt.Fprintf(os.Stdout, "#%d %s\nupdatedAt: %s\n", candidate.Number, candidate.Title, candidate.UpdatedAt); err != nil {
			return err
		}
		if candidate.ClosedAt != "" {
			if _, err := fmt.Fprintf(os.Stdout, "closedAt: %s\n", candidate.ClosedAt); err != nil {
				return err
			}
		}
		if candidate.URL != "" {
			if _, err := fmt.Fprintf(os.Stdout, "url: %s\n", candidate.URL); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(os.Stdout, "reason: %s\n", candidate.Reason)
		return err
	case "json":
		encoded, err := json.MarshalIndent(candidate, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, string(encoded))
		return err
	default:
		return usageError("next format must be one of number, text, or json")
	}
}

func runInspect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoValue := fs.String("repo", "", "GitHub repo in owner/name form")
	issueNumber := fs.Int("issue", 0, "closed issue number to inspect")
	format := fs.String("format", "markdown", "output format: markdown or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repoValue == "" {
		return usageError("inspect requires --repo")
	}
	if *issueNumber <= 0 {
		return usageError("inspect requires --issue")
	}

	repo, err := issuedocsync.ParseRepo(*repoValue)
	if err != nil {
		return err
	}
	client := issuedocsync.NewGitHubCLI()
	details, err := client.FetchIssueDetails(ctx, repo, *issueNumber)
	if err != nil {
		return err
	}
	return issuedocsync.WriteIssueDetails(os.Stdout, details, *format)
}

func runRecord(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoValue := fs.String("repo", "", "GitHub repo in owner/name form")
	statePath := fs.String("state-file", ".codex/state/issue-doc-sync/state.json", "tracked sync state file")
	issueNumber := fs.Int("issue", 0, "closed issue number")
	title := fs.String("title", "", "issue title")
	updatedAt := fs.String("updated-at", "", "GitHub issue updatedAt in RFC3339")
	closedAt := fs.String("closed-at", "", "GitHub issue closedAt in RFC3339")
	decision := fs.String("decision", "", "sync decision: skip, merge, or new-doc")
	reason := fs.String("reason", "", "decision reason")
	sourceURL := fs.String("source-url", "", "source issue URL")
	recordedAt := fs.String("recorded-at", "", "record timestamp in RFC3339; defaults to now")
	force := fs.Bool("force", false, "allow recording even when the target doc was already touched by a newer synced issue")
	var targetDocs multiFlag
	fs.Var(&targetDocs, "target-doc", "target doc path; repeat for multiple docs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repoValue == "" {
		return usageError("record requires --repo")
	}
	if *issueNumber <= 0 {
		return usageError("record requires --issue")
	}
	if *decision == "" {
		return usageError("record requires --decision")
	}
	if !issuedocsync.ValidDecision(*decision) {
		return usageError("record decision must be one of skip, merge, or new-doc")
	}
	if *reason == "" {
		return usageError("record requires --reason")
	}
	repo, err := issuedocsync.ParseRepo(*repoValue)
	if err != nil {
		return err
	}
	if *title == "" || *updatedAt == "" || *closedAt == "" || *sourceURL == "" {
		client := issuedocsync.NewGitHubCLI()
		details, err := client.FetchIssueDetails(ctx, repo, *issueNumber)
		if err != nil {
			return err
		}
		if *title == "" {
			*title = details.Title
		}
		if *updatedAt == "" {
			*updatedAt = details.UpdatedAt.UTC().Format(time.RFC3339)
		}
		if *closedAt == "" && !details.ClosedAt.IsZero() {
			*closedAt = details.ClosedAt.UTC().Format(time.RFC3339)
		}
		if *sourceURL == "" {
			*sourceURL = details.URL
		}
	}
	if *updatedAt == "" {
		return usageError("record requires --updated-at")
	}
	record := issuedocsync.IssueRecord{
		Number:     *issueNumber,
		Title:      *title,
		UpdatedAt:  *updatedAt,
		ClosedAt:   *closedAt,
		Decision:   *decision,
		Reason:     *reason,
		TargetDocs: targetDocs,
		SourceURL:  *sourceURL,
		RecordedAt: *recordedAt,
	}
	state, err := issuedocsync.LoadState(*statePath, repo.String())
	if err != nil {
		return err
	}
	if warnings := issuedocsync.FindRecordConflicts(state, record); len(warnings) > 0 && !*force {
		return fmt.Errorf("refusing to record due to overwrite risk:\n- %s\nrerun with --force if this backfill is intentional", strings.Join(warnings, "\n- "))
	}
	issuedocsync.UpsertRecord(&state, record)
	if err := issuedocsync.SaveState(*statePath, state); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(encoded))
	return err
}

type multiFlag []string

func (m *multiFlag) String() string {
	return fmt.Sprintf("%v", []string(*m))
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func usageError(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return errors.New(msg + "\nusage:\n  go run ./cmd/issue-doc-sync plan --repo owner/name [--state-file path] [--format text|json]\n  go run ./cmd/issue-doc-sync next --repo owner/name [--state-file path] [--format number|text|json]\n  go run ./cmd/issue-doc-sync inspect --repo owner/name --issue 22 [--format markdown|json]\n  go run ./cmd/issue-doc-sync record --repo owner/name --issue 22 --decision skip|merge|new-doc --reason text [--updated-at RFC3339] [--closed-at RFC3339] [--target-doc path ...] [--force]")
}
