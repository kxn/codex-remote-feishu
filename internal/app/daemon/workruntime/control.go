package workruntime

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ControlRequest identifies the authorized actor and frozen worker for recovery.
// It does not grant a human editor, terminal or desktop execution ownership.
type ControlRequest struct {
	ScopeID                      string
	WorkerID                     string
	AttemptID                    string
	ExpectedControllerGeneration int64
	Actor                        work.Actor
}

func (c *Coordinator) authorizeControl(ctx context.Context, request ControlRequest) error {
	if request.Actor.Validate() != nil || request.ScopeID == "" || request.WorkerID == "" || c.options.AuthorizeControl == nil {
		return fmt.Errorf("authorized scoped recovery actor required")
	}
	return c.options.AuthorizeControl(ctx, request.Actor, request.ScopeID, request.WorkerID)
}
