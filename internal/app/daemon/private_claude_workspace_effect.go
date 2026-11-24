package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/claudeworkspaceprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type privateClaudeWorkspaceReceipt struct {
	Version  int
	EffectID string
	Change   orchestrator.PrivateClaudeSnapshotChange
	Actual   state.ClaudeWorkspaceProfileSnapshotRecord
	Problem  string
}

func runPrivateClaudeWorkspace(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) error {
	effect, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Effect.Body)
	if err != nil {
		return err
	}
	cmd := event.DaemonCommand
	var change orchestrator.PrivateClaudeSnapshotChange
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateClaudeWorkspace || event.SurfaceSessionID != o.SurfaceID || cmd.SurfaceSessionID != o.SurfaceID || json.Unmarshal([]byte(cmd.Text), &change) != nil || change.SurfaceID != o.SurfaceID || change.Key == "" || change.Token == "" {
		return privatechat.ErrJournalConflict
	}
	if effect.State == "confirmed" {
		host.mu.Lock()
		store := host.claudeWorkspaceProfileState.store
		if store == nil || store.Path() == "" || host.claudeWorkspaceProfileState.status == persistedStoreStatusDegraded {
			host.mu.Unlock()
			return fmt.Errorf("Claude workspace profile store unavailable")
		}
		actual, _ := store.Get(change.Key)
		host.privateClaudeWorkspaceCache.Store(change.Key, actual)
		host.mu.Unlock()
		_, err = p.ApplyLocalHostReceipt(ctx, o, generation, id, privateClaudeWorkspaceReducer{host})
		return err
	}
	pending, err := j.EarlierClaudeWorkspaceEffectPending(ctx, o, generation, id, change.Key)
	if err != nil {
		return err
	}
	if pending {
		return privatechat.ErrDeferred
	}
	if effect.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return err
		}
	} else if effect.State != "sending" && effect.State != "unknown" {
		return privatechat.ErrJournalState
	}
	host.mu.Lock()
	store := host.claudeWorkspaceProfileState.store
	if store == nil || store.Path() == "" || host.claudeWorkspaceProfileState.status == persistedStoreStatusDegraded {
		host.mu.Unlock()
		return fmt.Errorf("Claude workspace profile store unavailable")
	}
	actual, _ := store.Get(change.Key)
	// The frozen delta applies only fields changed by this action. Earlier
	// committed effects for the key have already settled, so replay is safe.
	if change.Before.ReasoningEffort != change.After.ReasoningEffort {
		actual.ReasoningEffort = change.After.ReasoningEffort
	}
	if change.Before.AccessMode != change.After.AccessMode {
		actual.AccessMode = change.After.AccessMode
	}
	err = store.Put(change.Key, actual)
	problem := ""
	if err != nil {
		// A failed atomic write may still have reached disk. Inspect the source
		// file and settle known failure; only unreadable state remains unknown.
		loaded, readErr := claudeworkspaceprofile.LoadStore(store.Path())
		if readErr != nil {
			host.mu.Unlock()
			return readErr
		}
		host.claudeWorkspaceProfileState.store = loaded
		onDisk, _ := loaded.Get(change.Key)
		if onDisk != actual {
			problem = err.Error()
		}
		actual = onDisk
	}
	host.privateClaudeWorkspaceCache.Store(change.Key, actual)
	host.mu.Unlock()
	raw, err := json.Marshal(privateClaudeWorkspaceReceipt{Version: 1, EffectID: id, Change: change, Actual: actual, Problem: problem})
	if err != nil {
		return err
	}
	if err = j.ConfirmLocalEffect(context.WithoutCancel(ctx), o, generation, id, raw, privateLocalVerifier{o, generation, effect.Effect, raw}); err != nil {
		return err
	}
	_, err = p.ApplyLocalHostReceipt(ctx, o, generation, id, privateClaudeWorkspaceReducer{host})
	return err
}

type privateClaudeWorkspaceReducer struct{ host *App }

func (r privateClaudeWorkspaceReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var receipt privateClaudeWorkspaceReceipt
	var change orchestrator.PrivateClaudeSnapshotChange
	if event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandPrivateClaudeWorkspace || json.Unmarshal([]byte(event.DaemonCommand.Text), &change) != nil || json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != effect.ID || receipt.Change != change || change.SurfaceID != event.SurfaceSessionID {
		return nil, privatechat.ErrJournalConflict
	}
	if receipt.Problem == "" && (change.Before.ReasoningEffort != change.After.ReasoningEffort && receipt.Actual.ReasoningEffort != change.After.ReasoningEffort || change.Before.AccessMode != change.After.AccessMode && receipt.Actual.AccessMode != change.After.AccessMode) {
		return nil, privatechat.ErrJournalConflict
	}
	current, ok := r.host.privateClaudeWorkspaceCache.Load(change.Key)
	if !ok {
		return nil, privatechat.ErrDeferred
	}
	app.Service().ClearPrivateClaudeSnapshotOverlay(change)
	// Project the most recently read or written source value, never infer
	// freshness from equality with this potentially delayed receipt.
	if err := app.Service().ApplyPrivateClaudeSnapshotReceipt(change.Key, current.(state.ClaudeWorkspaceProfileSnapshotRecord)); err != nil {
		return nil, err
	}
	if receipt.Problem != "" {
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, SurfaceSessionID: change.SurfaceID, Notice: &control.Notice{Code: "claude_workspace_snapshot_failed", Title: "工作区偏好未保存", Text: "当前设置已处理，但工作区历史偏好未能保存；请检查服务日志后重试。"}}}, nil
	}
	return nil, nil
}
