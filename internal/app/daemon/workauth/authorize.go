package workauth

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// AuthorizeRun validates a signed credential against fresh durable state.
// The caller must load these facts in the same transaction as the operation.
// No fallback to a current attempt, surface, cwd, or gateway is permitted.
func AuthorizeRun(c Claims, runRef string, worker work.WorkerSession, attempt work.ExecutionAttempt, binding work.RoleBotBinding, gates []work.PauseGate) error {
	if err := authorizeOwner(c, runRef, worker, attempt, binding); err != nil {
		return err
	}
	if worker.Controller != work.ControllerAutomatic || worker.State != work.WorkerBusy || attempt.State != work.AttemptRunning {
		return ErrUnauthorized
	}
	allowed, err := work.GatesAllowRunForContext(worker.ScopeID, attempt.TaskID, worker.ContextID, gates)
	if err != nil || !allowed {
		return ErrUnauthorized
	}
	return nil
}

// AuthorizeReport permits an in-flight execution to preserve its account after
// dispatch has been paused. It grants no new business actions or control lease.
func AuthorizeReport(c Claims, runRef string, worker work.WorkerSession, attempt work.ExecutionAttempt, binding work.RoleBotBinding) error {
	if err := authorizeOwner(c, runRef, worker, attempt, binding); err != nil {
		return err
	}
	if worker.Controller != work.ControllerAutomatic || (worker.State != work.WorkerBusy && worker.State != work.WorkerRecovering) || (attempt.State != work.AttemptRunning && attempt.State != work.AttemptReconciling) {
		return ErrUnauthorized
	}
	return nil
}

func authorizeOwner(c Claims, runRef string, worker work.WorkerSession, attempt work.ExecutionAttempt, binding work.RoleBotBinding) error {
	if c.Purpose != "worker" || runRef == "" || c.RoleID != worker.RoleID || c.ScopeID != worker.ScopeID || c.ContextID != worker.ContextID || c.WorkerID != worker.ID || c.SessionGeneration != worker.Generation || c.ControllerGeneration != worker.ControllerGeneration {
		return ErrUnauthorized
	}
	if binding.RoleID != c.RoleID || binding.IdentityGeneration != c.BotGeneration {
		return ErrUnauthorized
	}
	if err := binding.Validate(); err != nil {
		return ErrUnauthorized
	}
	if err := work.ValidateAttemptBinding(attempt, worker); err != nil {
		return ErrUnauthorized
	}
	if attempt.RunRef.RunID != runRef {
		return ErrUnauthorized
	}
	return nil
}

// AuthorizeBootstrap only permits the original establishment operation and chat.
// After project creation, the service issues a new worker credential; claims
// cannot be edited or promoted by supplying a project ID in an API body.
func AuthorizeBootstrap(c Claims, tenant, chatID, principalID, operationID string, binding work.RoleBotBinding) error {
	if c.Purpose != "bootstrap" || c.Tenant != tenant || c.ChatID != chatID || c.PrincipalID != principalID || c.OperationID != operationID || binding.RoleID != c.RoleID || binding.IdentityGeneration != c.BotGeneration {
		return ErrUnauthorized
	}
	if err := binding.Validate(); err != nil {
		return ErrUnauthorized
	}
	return nil
}

// AuthorizeControlRun is only for the restricted management use cases. Normal
// business endpoints must reject a control-purpose context before AuthorizeRun.
func AuthorizeControlRun(c Claims, runRef string, worker work.WorkerSession, attempt work.ExecutionAttempt, binding work.RoleBotBinding, context work.WorkContext, project work.Project, gates []work.PauseGate) error {
	if err := authorizeOwner(c, runRef, worker, attempt, binding); err != nil {
		return err
	}
	if context.ID != worker.ContextID || context.ScopeID != worker.ScopeID || context.Purpose != work.PurposeControl || context.TaskID != "" || attempt.TaskID != "" || project.ScopeID != worker.ScopeID || project.LeaderRoleID != worker.RoleID || worker.Controller != work.ControllerAutomatic || worker.State != work.WorkerBusy || attempt.State != work.AttemptRunning {
		return ErrUnauthorized
	}
	allowed, err := work.GatesAllowRunForPurpose(worker.ScopeID, "", worker.ContextID, context.Purpose, gates)
	if err != nil || !allowed {
		return ErrUnauthorized
	}
	return nil
}

func AuthorizePersonalControlRun(c Claims, runRef string, worker work.WorkerSession, attempt work.ExecutionAttempt, binding work.RoleBotBinding, context work.WorkContext, scope work.WorkScope, gates []work.PauseGate) error {
	if err := authorizeOwner(c, runRef, worker, attempt, binding); err != nil {
		return err
	}
	if scope.Kind != work.ScopePersonal || scope.ID != worker.ScopeID || scope.RoleID != worker.RoleID || context.ID != worker.ContextID || context.Purpose != work.PurposeControl || context.ControlTargetContextID == "" || context.TaskID != "" || attempt.TaskID != "" || worker.Controller != work.ControllerAutomatic || worker.State != work.WorkerBusy || attempt.State != work.AttemptRunning {
		return ErrUnauthorized
	}
	allowed, err := work.GatesAllowRunForContext(scope.ID, "", context.ID, gates)
	if err != nil || !allowed {
		return ErrUnauthorized
	}
	return nil
}
