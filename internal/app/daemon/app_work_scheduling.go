package daemon

import (
	"context"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func (r *teamRuntime) tickScheduler(ctx context.Context) error {
	result, err := r.engine.Tick(ctx)
	if err != nil {
		return err
	}
	problems := make([]workstore.DispatchProblem, 0, len(result.Deferred))
	for _, item := range result.Deferred {
		problems = append(problems, workstore.DispatchProblem{ScopeID: item.Need.ScopeID, ContextID: item.Need.ContextID, RoleID: item.Need.RoleID, TaskID: item.Need.TaskID, Reason: item.Reason})
	}
	return r.store.Within(ctx, func(tx *workstore.Tx) error { return tx.ReplaceDispatchProblems(problems, time.Now().UTC()) })
}
