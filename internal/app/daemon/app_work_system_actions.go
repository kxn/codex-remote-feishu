package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) teamHostAction(ctx context.Context, source teamHumanSource, kind string, request any, execute func() (string, error)) (string, error) {
	title, refresh := "系统升级", "/upgrade"
	if kind == "host.upgrade.track" {
		title, refresh = "升级通道", "/upgrade track"
	}
	return a.teamHostPanelAction(ctx, source, kind, request, title, refresh, false, func() (workui.Panel, error) {
		result, err := execute()
		panel := workui.Panel{Title: title, Paragraphs: []string{result}, Buttons: []workui.Button{systemCommandButton("查看升级状态", "/upgrade"), systemCommandButton("升级通道", "/upgrade track")}}
		return panel, err
	})
}

func (a *App) teamHostPanelAction(ctx context.Context, source teamHumanSource, kind string, request any, title, refresh string, readOnly bool, execute func() (workui.Panel, error)) (string, error) {
	id := teamID("human-host", source.Key+":"+kind)
	send := false
	var panel workui.Panel
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := a.validateTeamPickerSource(tx, source); err != nil {
			return err
		}
		raw, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body := work.NewContent(id+":request", source.ScopeID, "application/json", raw, time.Now().UTC())
		if old, err := tx.GetOperation(id); err == nil {
			if old.RequestHash != body.SHA256 {
				return workstore.ErrIdempotencyConflict
			}
			if old.State == work.OperationConfirmed {
				panel = decodeSystemPanel(old.ExternalReceipt, title)
				if len(panel.Buttons) == 0 && refresh != "" {
					panel.Buttons = []workui.Button{systemCommandButton("刷新状态", refresh)}
				}
				return nil
			}
			problem := old.LastError
			if problem == "" {
				problem = "操作已受理，结果尚待核对"
			}
			panel = systemFailurePanel(title, refresh, fmt.Errorf("%s", problem), old.State != work.OperationTerminal)
			return nil
		} else if err != workstore.ErrNotFound {
			return err
		}
		if err = tx.PutContent(body); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{ID: id, ScopeID: source.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, Kind: kind, TargetID: "installation", TargetGeneration: 1, IdempotencyKey: source.Key, RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, 0)
		send = err == nil
		return err
	})
	if err != nil {
		return "", err
	}
	if send {
		var problem error
		panel, problem = execute()
		if problem != nil {
			panel = systemFailurePanel(title, refresh, problem, !readOnly)
		}
		encoded, err := json.Marshal(panel)
		if err != nil {
			return "", err
		}
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = a.teamRuntime.store.Within(saveCtx, func(tx *workstore.Tx) error {
			op, err := tx.GetOperation(id)
			if err != nil {
				return err
			}
			before := op.Revision
			op.Revision++
			if problem != nil {
				op.State = work.OperationUnknown
				if readOnly {
					op.State = work.OperationTerminal
				}
				op.LastError = problem.Error()
			} else {
				op.State = work.OperationConfirmed
				op.ExternalReceipt = string(encoded)
			}
			op.UpdatedAt = time.Now().UTC()
			_, _, err = tx.PutOperation(op, before)
			return err
		})
		cancel()
		if err != nil {
			return "", err
		}
	}
	return a.queueTeamSystemPanel(ctx, source, panel)
}
