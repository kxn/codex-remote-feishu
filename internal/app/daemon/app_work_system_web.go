package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type systemWebRequest struct {
	Source teamHumanSource `json:"source"`
}

func (a *App) queueTeamSystemWeb(ctx context.Context, source teamHumanSource) (string, error) {
	id := teamID("system-web", source.Key)
	viewID := workui.ID(source.Key, "system", "管理页")
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, err := tx.GetOperation(id); err == nil {
			return nil
		} else if err != workstore.ErrNotFound {
			return err
		}
		panel := workui.Panel{Title: "管理页", Paragraphs: []string{"正在准备管理页链接。首次启动外网通道可能需要几十秒，结果会在本页更新；你可以继续使用其他菜单。"}}
		if err := a.queueTeamSystemPanelTx(tx, source, panel, "private.navigation", source.ScopeID, viewID); err != nil {
			return err
		}
		raw, err := json.Marshal(systemWebRequest{Source: source})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		body := work.NewContent(id+":request", source.ScopeID, "application/json", raw, now)
		if err = tx.PutContent(body); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{ID: id, ScopeID: source.ScopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "system-menu"}, Kind: "host.admin.weblink", TargetID: "installation", TargetGeneration: 1, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: id, State: work.OperationPrepared, Steps: []work.OperationStep{{ID: "ui_pending", State: work.OperationConfirmed, Receipt: viewID}}, Revision: 1, CreatedAt: now, UpdatedAt: now}, 0)
		return err
	})
	return viewID, err
}

// The runtime invokes this in one independent serial lane. No request starts a
// goroutine and no interrupted sending/unknown tunnel issuance is replayed.
func (a *App) runTeamSystemHostJobs(ctx context.Context) error {
	var pending []work.Operation
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var err error; pending, err = tx.ListSystemHostJobs(); return err }); err != nil {
		return err
	}
	dispatched := false
	for _, job := range pending {
		if job.State == work.OperationPrepared {
			if dispatched {
				continue
			}
			dispatched = true
			if err := a.executeSystemWebJob(ctx, job.ID); err != nil {
				return err
			}
		} else if err := a.presentSystemWebJob(ctx, job.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) executeSystemWebJob(ctx context.Context, id string) error {
	var scopeID string
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		scopeID = op.ScopeID
		return nil
	}); err != nil {
		return err
	}
	end, err := a.teamRuntime.beginPersonalSourceActivity(ctx, scopeID)
	if errors.Is(err, workstore.ErrPersonalMigrationFenced) {
		return nil
	}
	if err != nil {
		return err
	}
	defer end()
	var request systemWebRequest
	send := false
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		if op.State != work.OperationPrepared {
			return nil
		}
		body, err := tx.GetContent(op.ScopeID, op.RequestRef)
		if err != nil {
			return err
		}
		if body.SHA256 != op.RequestHash {
			return workstore.ErrConflict
		}
		if err = json.Unmarshal(body.Data, &request); err != nil {
			return err
		}
		viewID := ""
		for _, step := range op.Steps {
			if step.ID == "ui_pending" {
				viewID = step.Receipt
			}
		}
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		binding, err := tx.GetRoleBotBinding(view.Binding.RoleID)
		if err != nil {
			return err
		}
		if view.ScopeID != op.ScopeID || view.ScopeID != request.Source.ScopeID || view.PrincipalID != request.Source.PrincipalID || view.Binding.RoleID != request.Source.RoleID {
			return workstore.ErrScopeDenied
		}
		nav, err := tx.GetPrivateNavigation(op.ScopeID)
		if err != nil {
			return err
		}
		previous := op.Revision
		op.Revision++
		op.UpdatedAt = time.Now().UTC()
		if view.Invalidated || nav.Revision != view.TargetRevision || binding != view.Binding {
			op.State = work.OperationTerminal
			op.LastError = "已离开原页面，尚未开始生成新链接"
			op.Steps = append(op.Steps, work.OperationStep{ID: "ui_result", State: work.OperationConfirmed, Receipt: "stale-before-send"})
		} else {
			op.State = work.OperationSending
			send = true
		}
		_, _, err = tx.PutOperation(op, previous)
		return err
	})
	if err != nil || !send {
		return err
	}
	issueCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	panel, problem := a.issueSystemAdminLink(issueCtx)
	cancel()
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	err = a.teamRuntime.store.Within(saveCtx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		previous := op.Revision
		op.Revision++
		op.UpdatedAt = time.Now().UTC()
		if problem != nil {
			op.State = work.OperationUnknown
			op.LastError = problem.Error()
		} else {
			raw, err := json.Marshal(panel)
			if err != nil {
				return err
			}
			op.State = work.OperationConfirmed
			op.ExternalReceipt = string(raw)
		}
		_, _, err = tx.PutOperation(op, previous)
		return err
	})
	if err != nil {
		return err
	}
	return a.presentSystemWebJob(saveCtx, id)
}

func (a *App) presentSystemWebJob(ctx context.Context, id string) error {
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		if fenceErr := tx.AssertPersonalMigrationOpen(op.ScopeID); errors.Is(fenceErr, workstore.ErrPersonalMigrationFenced) {
			if op.State == work.OperationSending {
				before := op.Revision
				op.Revision++
				op.State = work.OperationUnknown
				op.LastError = "先前生成链接的过程已中断，可能已创建通道或链接"
				_, _, err = tx.PutOperation(op, before)
				return err
			}
			return nil
		} else if fenceErr != nil {
			return fenceErr
		}
		for _, step := range op.Steps {
			if step.ID == "ui_result" {
				return nil
			}
		}
		if op.State == work.OperationSending {
			before := op.Revision
			op.Revision++
			op.State = work.OperationUnknown
			op.LastError = "先前生成链接的过程已中断，可能已创建通道或链接"
			if _, _, err = tx.PutOperation(op, before); err != nil {
				return err
			}
		}
		panel := systemWebJobPanel(op)
		if !strings.Contains(panel.Title, "/admin web") {
			panel.Title += " /admin web"
		}
		panel.PrivateMenu = true
		panel.Breadcrumb = "菜单首页 › 系统管理 › 管理页"
		panel.Navigation = []workui.Button{{Label: "返回系统管理", Action: "private.admin"}, {Label: "返回菜单首页", Action: "private.menu"}}
		panel.Footer = []workui.Button{{Label: "当前状态", Action: "private.status"}}
		return a.presentTeamPrivateNativeResult(tx, op, panel, time.Now().UTC(), false)
	})
}

func systemWebJobPanel(op work.Operation) workui.Panel {
	if op.State == work.OperationConfirmed {
		return decodeSystemPanel(op.ExternalReceipt)
	}
	return workui.Panel{Title: "管理页 /admin web", Paragraphs: []string{"管理页链接生成结果尚未确认：" + op.LastError, "可能已创建通道或链接；本次操作不会自动重发。需要新链接时，请明确选择重新生成。"}, Buttons: []workui.Button{{Label: "核对链接生成状态", Action: "system.web-status", Values: map[string]any{"job_id": op.ID}}, systemCommandButton("重新生成管理页链接（再次操作）", "/admin web")}}
}

func (a *App) systemWebStatus(ctx context.Context, source teamHumanSource, id string) (string, error) {
	var op work.Operation
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		op, err = tx.GetOperation(id)
		if err != nil {
			return err
		}
		if (op.Kind != "host.admin.weblink" && op.Kind != "host.admin.web") || op.ScopeID != source.ScopeID {
			return workstore.ErrScopeDenied
		}
		return nil
	}); err != nil {
		return "", err
	}
	panel := systemWebJobPanel(op)
	if op.Kind == "host.admin.web" {
		panel = legacySystemWebPanel(op)
	}
	if op.Kind == "host.admin.weblink" && (op.State == work.OperationPrepared || op.State == work.OperationSending) {
		panel = workui.Panel{Title: "管理页生成状态", Paragraphs: []string{fmt.Sprintf("当前记录状态：%s；尚未确认生成结果。", op.State)}}
	}
	return a.queueTeamSystemPanel(ctx, source, panel)
}
