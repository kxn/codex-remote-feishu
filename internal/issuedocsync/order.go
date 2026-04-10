package issuedocsync

import (
	"sort"
	"time"
)

func SortPlanCandidatesOldestFirst(candidates []PlanCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if cmp := compareIssueOrder(left.ClosedAt, left.UpdatedAt, left.Number, right.ClosedAt, right.UpdatedAt, right.Number); cmp != 0 {
			return cmp < 0
		}
		return left.Title < right.Title
	})
}

func compareIssueRecordsForSync(a, b IssueRecord) int {
	return compareIssueOrder(a.ClosedAt, a.UpdatedAt, a.Number, b.ClosedAt, b.UpdatedAt, b.Number)
}

func compareIssueOrder(leftClosedAt string, leftUpdatedAt string, leftNumber int, rightClosedAt string, rightUpdatedAt string, rightNumber int) int {
	leftTime := issueOrderTime(leftClosedAt, leftUpdatedAt)
	rightTime := issueOrderTime(rightClosedAt, rightUpdatedAt)
	if !leftTime.Equal(rightTime) {
		if leftTime.Before(rightTime) {
			return -1
		}
		return 1
	}
	switch {
	case leftNumber < rightNumber:
		return -1
	case leftNumber > rightNumber:
		return 1
	default:
		return 0
	}
}

func issueOrderTime(closedAt string, updatedAt string) time.Time {
	if parsed, ok := parseRFC3339(closedAt); ok {
		return parsed
	}
	if parsed, ok := parseRFC3339(updatedAt); ok {
		return parsed
	}
	return time.Time{}
}

func parseRFC3339(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
