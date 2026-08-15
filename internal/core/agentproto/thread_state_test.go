package agentproto

import (
	"testing"
)

func int64Ptr(value int64) *int64 {
	return &value
}

func TestNormalizeThreadGoalUpdatePreservesFullSnapshot(t *testing.T) {
	update := NormalizeThreadGoalUpdate(&ThreadGoalUpdate{
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		Objective:       "ship it",
		Status:          "active",
		TokenBudget:     int64Ptr(1200),
		TokensUsed:      345,
		TimeUsedSeconds: 67,
		CreatedAt:       1710000000123,
		UpdatedAt:       1710000000999,
	})
	if update == nil {
		t.Fatal("expected normalized goal update")
	}
	if update.ThreadID != "thread-1" || update.Objective != "ship it" || update.Status != "active" {
		t.Fatalf("unexpected normalized goal: %#v", update)
	}
	if update.TokenBudget == nil || *update.TokenBudget != 1200 {
		t.Fatalf("token budget = %#v, want 1200", update.TokenBudget)
	}
	if update.TokensUsed != 345 || update.TimeUsedSeconds != 67 || update.CreatedAt != 1710000000123 || update.UpdatedAt != 1710000000999 {
		t.Fatalf("usage/timestamps lost: %#v", update)
	}
}

func TestNormalizeThreadGoalUpdateClearedClearsGoalFields(t *testing.T) {
	update := NormalizeThreadGoalUpdate(&ThreadGoalUpdate{
		ThreadID:        "thread-1",
		Objective:       "ship it",
		Status:          "active",
		TokenBudget:     int64Ptr(1200),
		TokensUsed:      345,
		TimeUsedSeconds: 67,
		CreatedAt:       1710000000123,
		UpdatedAt:       1710000000999,
		Cleared:         true,
	})
	if update == nil || !update.Cleared {
		t.Fatalf("expected cleared goal update, got %#v", update)
	}
	if update.Objective != "" || update.Status != "" || update.TokenBudget != nil ||
		update.TokensUsed != 0 || update.TimeUsedSeconds != 0 || update.CreatedAt != 0 || update.UpdatedAt != 0 {
		t.Fatalf("cleared goal retained fields: %#v", update)
	}
}

func TestNormalizeThreadGoalUpdateKeepsExternalMutationFlag(t *testing.T) {
	update := NormalizeThreadGoalUpdate(&ThreadGoalUpdate{
		ThreadID:         "thread-1",
		Status:           "paused",
		ExternalMutation: true,
	})
	if update == nil || !update.ExternalMutation {
		t.Fatalf("expected external mutation flag to survive normalization, got %#v", update)
	}
}

func TestCloneThreadGoalUpdateDeepCopiesTokenBudget(t *testing.T) {
	original := &ThreadGoalUpdate{
		ThreadID:    "thread-1",
		Status:      "active",
		TokenBudget: int64Ptr(1200),
	}
	cloned := CloneThreadGoalUpdate(original)
	if cloned == nil || cloned.TokenBudget == nil || *cloned.TokenBudget != 1200 {
		t.Fatalf("unexpected clone: %#v", cloned)
	}
	*cloned.TokenBudget = 999
	if *original.TokenBudget != 1200 {
		t.Fatal("clone must deep copy token budget pointer")
	}
}
