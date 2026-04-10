package issuedocsync

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

func BuildPlanReport(repo string, summaries []IssueSummary, state StateFile) PlanReport {
	report := PlanReport{
		Repo:             repo,
		ScannedClosed:    len(summaries),
		CachedIssueCount: len(state.Issues),
		Candidates:       make([]PlanCandidate, 0),
	}
	for _, summary := range summaries {
		key := fmt.Sprintf("%d", summary.Number)
		record, ok := state.Issues[key]
		currentUpdatedAt := summary.UpdatedAt.UTC().Format(time.RFC3339)
		if !ok {
			report.Candidates = append(report.Candidates, PlanCandidate{
				Number:    summary.Number,
				Title:     summary.Title,
				UpdatedAt: currentUpdatedAt,
				ClosedAt:  formatTimeRFC3339(summary.ClosedAt),
				URL:       summary.URL,
				Reason:    "not yet recorded in tracked issue-doc sync state",
			})
			continue
		}
		if record.UpdatedAt != currentUpdatedAt {
			report.Candidates = append(report.Candidates, PlanCandidate{
				Number:            summary.Number,
				Title:             summary.Title,
				UpdatedAt:         currentUpdatedAt,
				ClosedAt:          formatTimeRFC3339(summary.ClosedAt),
				URL:               summary.URL,
				Reason:            "issue updated since the recorded sync decision",
				PreviousUpdatedAt: record.UpdatedAt,
			})
		}
	}
	SortPlanCandidatesOldestFirst(report.Candidates)
	report.CandidateCount = len(report.Candidates)
	return report
}

func WritePlanReport(w io.Writer, report PlanReport, format string) error {
	switch format {
	case "json":
		payload, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(payload))
		return err
	case "text":
		if _, err := fmt.Fprintf(w, "repo: %s\nclosed issues scanned: %d\ntracked cache entries: %d\ncandidates: %d\nprocessing order: oldest closed issue first\n", report.Repo, report.ScannedClosed, report.CachedIssueCount, report.CandidateCount); err != nil {
			return err
		}
		if len(report.Candidates) == 0 {
			_, err := fmt.Fprintln(w, "no changed closed issues need review")
			return err
		}
		for _, candidate := range report.Candidates {
			if _, err := fmt.Fprintf(w, "- #%d %s\n  updatedAt: %s\n  reason: %s\n", candidate.Number, candidate.Title, candidate.UpdatedAt, candidate.Reason); err != nil {
				return err
			}
			if candidate.ClosedAt != "" {
				if _, err := fmt.Fprintf(w, "  closedAt: %s\n", candidate.ClosedAt); err != nil {
					return err
				}
			}
			if candidate.PreviousUpdatedAt != "" {
				if _, err := fmt.Fprintf(w, "  previousUpdatedAt: %s\n", candidate.PreviousUpdatedAt); err != nil {
					return err
				}
			}
			if candidate.URL != "" {
				if _, err := fmt.Fprintf(w, "  url: %s\n", candidate.URL); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func formatTimeRFC3339(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func WriteIssueDetails(w io.Writer, details IssueDetails, format string) error {
	switch format {
	case "json":
		payload, err := json.MarshalIndent(details, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(payload))
		return err
	case "markdown":
		if _, err := fmt.Fprintf(w, "# Issue #%d %s\n\n", details.Number, details.Title); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "- updatedAt: %s\n", details.UpdatedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
		if !details.ClosedAt.IsZero() {
			if _, err := fmt.Fprintf(w, "- closedAt: %s\n", details.ClosedAt.UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		}
		if details.URL != "" {
			if _, err := fmt.Fprintf(w, "- url: %s\n", details.URL); err != nil {
				return err
			}
		}
		if len(details.Labels) > 0 {
			if _, err := fmt.Fprintf(w, "- labels: %s\n", strings.Join(details.Labels, ", ")); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, "\n## Body\n\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, details.Body); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n## Comments\n\n"); err != nil {
			return err
		}
		if len(details.Comments) == 0 {
			_, err := fmt.Fprintln(w, "_No comments_")
			return err
		}
		for _, comment := range details.Comments {
			author := comment.Author
			if author == "" {
				author = "unknown"
			}
			if _, err := fmt.Fprintf(w, "### %s @%s\n\n", comment.PublishedAt.UTC().Format(time.RFC3339), author); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(w, comment.Body); err != nil {
				return err
			}
			if comment.URL != "" {
				if _, err := fmt.Fprintf(w, "\nsource: %s\n\n", comment.URL); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}
