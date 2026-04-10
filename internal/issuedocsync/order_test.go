package issuedocsync

import "testing"

func TestSortPlanCandidatesOldestFirstByClosedAtThenNumber(t *testing.T) {
	candidates := []PlanCandidate{
		{Number: 42, Title: "newest", UpdatedAt: "2026-04-09T10:00:00Z", ClosedAt: "2026-04-09T09:00:00Z"},
		{Number: 41, Title: "older", UpdatedAt: "2026-04-08T10:00:00Z", ClosedAt: "2026-04-08T09:00:00Z"},
		{Number: 40, Title: "same-close-lower-number", UpdatedAt: "2026-04-08T12:00:00Z", ClosedAt: "2026-04-08T09:00:00Z"},
	}

	SortPlanCandidatesOldestFirst(candidates)

	want := []int{40, 41, 42}
	for i, number := range want {
		if candidates[i].Number != number {
			t.Fatalf("candidates[%d] = %#v, want issue %d", i, candidates[i], number)
		}
	}
}

func TestSortPlanCandidatesFallsBackToUpdatedAtWhenClosedAtMissing(t *testing.T) {
	candidates := []PlanCandidate{
		{Number: 51, Title: "newer", UpdatedAt: "2026-04-09T10:00:00Z"},
		{Number: 50, Title: "older", UpdatedAt: "2026-04-08T10:00:00Z"},
	}

	SortPlanCandidatesOldestFirst(candidates)

	if candidates[0].Number != 50 || candidates[1].Number != 51 {
		t.Fatalf("unexpected order: %#v", candidates)
	}
}
