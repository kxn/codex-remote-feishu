package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type privateWorktreeReceipt struct {
	Version             int
	EffectID, SurfaceID string
	Observation         orchestrator.PrivateWorktreeObservation
}

func runPrivateWorktreeObservation(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateWorktreeObserve || event.SurfaceSessionID != o.SurfaceID || cmd.SurfaceSessionID != o.SurfaceID {
		return nil, privatechat.ErrJournalConflict
	}
	var action control.Action
	if err = json.Unmarshal([]byte(cmd.Text), &action); err != nil {
		return nil, err
	}
	if action.GatewayID != o.GatewayID || action.SurfaceSessionID != o.SurfaceID || action.ChatID != o.ChatID || action.ActorUserID != o.UserID {
		return nil, privatechat.ErrJournalConflict
	}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateWorktreeReducer{host})
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, fmt.Errorf("worktree observation requires recovery")
	}
	var request gitmeta.WorktreeCreateRequest
	if err = p.InspectLocalObservation(ctx, o, generation, id, func(app *privatechat.App, _ privatechat.EffectRecord) error {
		if err := app.ValidateAction(action); err != nil {
			return err
		}
		var e error
		request, e = app.Service().PrivateWorktreeRequest(action)
		return e
	}); err != nil {
		return nil, err
	}
	observation := orchestrator.PrivateWorktreeObservation{Request: request}
	if request.BaseWorkspacePath != "" {
		observation.Result, err = gitmeta.PreviewWorktree(request)
		if err != nil {
			var detail *gitmeta.WorktreeCreateError
			if errors.As(err, &detail) {
				observation.ErrorCode = detail.Code
				observation.ErrorDestination = detail.DestinationPath
				if observation.ErrorCode == "" {
					observation.ErrorCode = gitmeta.WorktreeCreateErrorCreateFailed
				}
			} else {
				observation.ErrorCode = gitmeta.WorktreeCreateErrorCreateFailed
			}
		}
	}
	receipt, err := json.Marshal(privateWorktreeReceipt{Version: 1, EffectID: id, SurfaceID: o.SurfaceID, Observation: observation})
	if err != nil {
		return nil, err
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	verifier := privateLocalVerifier{o, generation, record.Effect, receipt}
	if err = j.ConfirmLocalEffect(context.WithoutCancel(ctx), o, generation, id, receipt, verifier); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateWorktreeReducer{host})
}

type privateWorktreeReducer struct{ host *App }

func (r privateWorktreeReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	var receipt privateWorktreeReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateWorktreeObserve || receipt.Version != 1 || receipt.EffectID != effect.ID || receipt.SurfaceID != event.SurfaceSessionID {
		return nil, privatechat.ErrJournalConflict
	}
	var action control.Action
	if err = json.Unmarshal([]byte(cmd.Text), &action); err != nil {
		return nil, err
	}
	if err = app.ValidateAction(action); err != nil {
		return nil, err
	}
	events, err := app.Service().ApplyPrivateWorktreeObservation(action, receipt.Observation)
	if errors.Is(err, orchestrator.ErrPrivateWorktreeStale) {
		return nil, nil
	}
	return events, err
}
