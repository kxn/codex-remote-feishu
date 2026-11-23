package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// Only synchronous local executors belong here. Native dispatch, publication,
// project and headless lifecycles own different reconciliation. Upgrade
// executors return after their durable helper intent is independently tracked.
func localEffectReturnsAfterHostCall(kind control.DaemonCommandKind) bool {
	switch kind {
	case privatePreviewRetryKind, control.DaemonCommandPrivateMkdir,
		control.DaemonCommandPrivateBotSettings, control.DaemonCommandPrivateStageInput,
		control.DaemonCommandPrivateWorktreeObserve, control.DaemonCommandGitWorkspaceImport,
		control.DaemonCommandGitWorkspaceWorktreeCreate, control.DaemonCommandGitWorkspaceImportCancel,
		control.DaemonCommandGitWorkspaceWorktreeCancel, control.DaemonCommandAdmin,
		control.DaemonCommandDebug, control.DaemonCommandUpgrade, control.DaemonCommandUpgradeOwnerFlow,
		control.DaemonCommandSendIMFile, control.DaemonCommandPrivateSendFile:
		return true
	default:
		return false
	}
}

// The runner holds this process-local evidence only after the synchronous call
// returns. A restart instead uses the journal's existing sending recovery.
func (r *privateWorkRunner) rememberReturnedLocalEffect(work privatechat.PendingEffectWork) {
	r.effectMu.Lock()
	defer r.effectMu.Unlock()
	if r.returnedLocal == nil {
		r.returnedLocal = make(map[string]privatechat.PendingEffectWork)
	}
	r.returnedLocal[work.Effect.Effect.ID] = work
}
func (r *privateWorkRunner) hasReturnedLocalEffect(id string) bool {
	r.effectMu.Lock()
	defer r.effectMu.Unlock()
	_, ok := r.returnedLocal[id]
	return ok
}
func (r *privateWorkRunner) retryReturnedLocalEffect(ctx context.Context, work privatechat.PendingEffectWork) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, err := r.journal.LoadEffect(saveCtx, work.Owner, work.Generation, work.Effect.Effect.ID)
	if err != nil {
		return err
	}
	if current.State == "sending" {
		body, _ := json.Marshal(map[string]string{"reason": "local_call_returned_before_receipt"})
		if err = r.journal.ResolveEffect(saveCtx, work.Owner, work.Generation, work.Effect.Effect.ID, "unknown", body, nil); err != nil {
			return err
		}
	}
	r.effectMu.Lock()
	delete(r.returnedLocal, work.Effect.Effect.ID)
	r.effectMu.Unlock()
	return nil
}
func (r *privateWorkRunner) runLocalEffectWithRecovery(ctx context.Context, work privatechat.PendingEffectWork, run func() error) error {
	if r.hasReturnedLocalEffect(work.Effect.Effect.ID) {
		return r.retryReturnedLocalEffect(ctx, work)
	}
	if work.Effect.State != "prepared" {
		return run()
	}
	before, err := r.journal.LoadEffect(ctx, work.Owner, work.Generation, work.Effect.Effect.ID)
	if err != nil {
		return err
	}
	if before.State != "prepared" {
		return run()
	}
	err = run()
	if err == nil {
		return nil
	}
	r.rememberReturnedLocalEffect(work)
	return errors.Join(err, r.retryReturnedLocalEffect(ctx, work))
}
