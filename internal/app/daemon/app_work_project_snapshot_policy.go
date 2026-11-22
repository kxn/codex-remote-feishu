package daemon

import (
	"encoding/json"
	"slices"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Only new blockers and lifecycle transitions take the interactive lane.
// A persistent blocker cannot make ordinary task-count changes high-frequency.
func projectSnapshotUrgent(previous []byte, facts teamProjectProjection) bool {
	var before struct {
		State    work.ProjectState
		Problems []string
	}
	if len(previous) == 0 {
		return len(facts.Problems) > 0
	}
	if json.Unmarshal(previous, &before) != nil {
		return false
	}
	return before.State != facts.Project.State || len(facts.Problems) > 0 && !slices.Equal(before.Problems, facts.Problems)
}
