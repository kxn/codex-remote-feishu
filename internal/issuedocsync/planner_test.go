package issuedocsync

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBuildPlanReportDetectsUncachedAndChangedIssues(t *testing.T) {
	now := time.Date(2026, 4, 8, 4, 0, 0, 0, time.UTC)
	summaries := []IssueSummary{
		{Number: 40, Title: "sync docs", UpdatedAt: now, URL: "https://example.com/40"},
		{Number: 39, Title: "already cached", UpdatedAt: now.Add(-time.Hour), URL: "https://example.com/39"},
		{Number: 38, Title: "changed issue", UpdatedAt: now.Add(-2 * time.Hour), URL: "https://example.com/38"},
	}
	state := StateFile{
		Version: 1,
		Repo:    "kxn/codex-remote-feishu",
		Issues: map[string]IssueRecord{
			"39": {Number: 39, UpdatedAt: now.Add(-time.Hour).Format(time.RFC3339)},
			"38": {Number: 38, UpdatedAt: now.Add(-3 * time.Hour).Format(time.RFC3339)},
		},
	}

	report := BuildPlanReport("kxn/codex-remote-feishu", summaries, state)
	if report.CandidateCount != 2 {
		t.Fatalf("candidate count = %d, want 2", report.CandidateCount)
	}
	if report.Candidates[0].Number != 38 {
		t.Fatalf("first candidate = %#v, want issue 38", report.Candidates[0])
	}
	if report.Candidates[0].Reason != "issue updated since the recorded sync decision" {
		t.Fatalf("unexpected changed reason: %#v", report.Candidates[0])
	}
	if report.Candidates[1].Number != 40 {
		t.Fatalf("second candidate = %#v, want issue 40", report.Candidates[1])
	}
	if report.Candidates[0].PreviousUpdatedAt == "" {
		t.Fatalf("expected changed issue to include previousUpdatedAt, got %#v", report.Candidates[0])
	}
}

func TestWritePlanReportText(t *testing.T) {
	report := PlanReport{
		Repo:             "kxn/codex-remote-feishu",
		ScannedClosed:    3,
		CachedIssueCount: 1,
		CandidateCount:   1,
		Candidates: []PlanCandidate{
			{
				Number:    22,
				Title:     "Headless instance 改用 pool 管理",
				UpdatedAt: "2026-04-08T02:29:31Z",
				Reason:    "not yet recorded in tracked issue-doc sync state",
				URL:       "https://example.com/22",
			},
		},
	}

	var buf bytes.Buffer
	if err := WritePlanReport(&buf, report, "text"); err != nil {
		t.Fatalf("WritePlanReport(text) error = %v", err)
	}
	output := buf.String()
	for _, fragment := range []string{
		"repo: kxn/codex-remote-feishu",
		"candidates: 1",
		"processing order: oldest closed issue first",
		"#22",
		"Headless instance 改用 pool 管理",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("output missing %q:\n%s", fragment, output)
		}
	}
}
