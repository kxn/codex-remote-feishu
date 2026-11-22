package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) queueTeamMCPRead(ctx context.Context, source teamHumanSource, query agentproto.RuntimeMCPQuery) (string, error) {
	id := teamID("private-mcp-read", source.Key)
	viewID := workui.ID(id, "pending")
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if prior, err := tx.GetOperation(id); err == nil {
			body, err := tx.GetContent(prior.ScopeID, prior.RequestRef)
			if err != nil {
				return err
			}
			var command agentproto.Command
			if err = json.Unmarshal(body.Data, &command); err != nil {
				return err
			}
			if command.RuntimeMCP == nil || *command.RuntimeMCP != query {
				return workstore.ErrIdempotencyConflict
			}
			return nil
		} else if err != workstore.ErrNotFound {
			return err
		}
		front, revision, err := worksettings.FrozenContext(tx, source.Authority)
		if err != nil {
			return err
		}
		_, human, err := tx.ValidatePrivateAction(source.Authority)
		if err != nil {
			return err
		}
		worker, carrier, err := a.teamNativeTarget(tx, source)
		if err != nil {
			return err
		}
		binding, err := tx.GetRoleBotBinding(source.RoleID)
		if err != nil {
			return err
		}
		owner := carrier.Owner
		now := time.Now().UTC()
		command := agentproto.Command{CommandID: id, Kind: agentproto.CommandRuntimeMCPRead, Execution: &owner, Target: agentproto.Target{ThreadID: worker.NativeSessionID}, RuntimeMCP: &query, IssuedAt: now}
		raw, err := json.Marshal(command)
		if err != nil {
			return err
		}
		body := work.NewContent(id+":request", worker.ScopeID, "application/json", raw, now)
		if err = tx.PutContent(body); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{ID: id, ScopeID: worker.ScopeID, ContextID: front.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "user-session-mcp"}, Kind: "native.user.mcp.read", TargetID: worker.ID, TargetGeneration: worker.Generation, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: id, State: work.OperationPrepared, Steps: []work.OperationStep{{ID: "human_source", State: work.OperationConfirmed, Receipt: stringMustJSON(human)}, {ID: "carrier", State: work.OperationConfirmed, Receipt: carrier.Incarnation}, {ID: "ui_pending", State: work.OperationConfirmed, Receipt: viewID}}, Revision: 1, CreatedAt: now, UpdatedAt: now}, 0)
		if err != nil {
			return err
		}
		panel := teamMCPReadPanel([]string{"正在读取当前会话的 MCP 服务。可以继续使用其他命令；结果将在此页更新。"})
		view := workstore.UIView{ID: viewID, ScopeID: source.ScopeID, ContextID: front.ID, Kind: "private.navigation", TargetID: source.ScopeID, TargetRevision: revision, Binding: binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: source.PrincipalID, ReplyToMessageID: source.MessageID}
		if err = workui.PrivateNavigationFromInteraction(tx, &view, source.Authority.InteractionID); err != nil {
			return err
		}
		card, err := workui.Card(viewID, panel)
		if err != nil {
			return err
		}
		_, err = workui.Queue(tx, view, card)
		return err
	})
	if errors.Is(err, errTeamNoNativeSession) {
		return a.queuePrivatePanel(ctx, source, "mcp", "当前工作还没有原生会话。开始工作后，可在这里查看加载的服务；本次查询没有启动执行。")
	}
	return viewID, err
}

func teamMCPReadPanel(lines []string) workui.Panel {
	return workui.Panel{Title: "MCP 服务", Paragraphs: lines, Buttons: []workui.Button{{Label: "刷新服务状态", Action: "private.mcp-status"}, {Label: "返回 MCP 服务与登录", Action: "private.mcp"}, {Label: "返回常用工具", Action: "private.tools"}}}
}

func teamMCPReadLines(result agentproto.RuntimeMCPResult) []string {
	if !result.Supported {
		return []string{"当前后端没有提供这项 MCP 查询能力。"}
	}
	lines := []string{"当前会话的 MCP 服务："}
	for _, server := range result.Servers {
		line := server.Name
		if server.Status != "" {
			line += " · " + server.Status
		}
		if server.Authentication != "" {
			line += " · 认证：" + server.Authentication
		}
		if result.Mode == "configuration" {
			line += " · " + server.Transport
			if server.Origin != "" {
				line += " · " + server.Origin
			}
		}
		lines = append(lines, line)
	}
	if len(result.Servers) == 0 {
		lines = append(lines, "没有已加载的 MCP 服务。")
	}
	if result.Problem != nil {
		lines = append(lines, "部分服务暂时无法核对："+result.Problem.Message)
	}
	if result.NextCursor != "" {
		verb := "status"
		if result.Mode == "configuration" {
			verb = "config"
		}
		lines = append(lines, "还有更多服务；可用 /mcp "+verb+" --cursor "+result.NextCursor+" 查看后续页。")
	}
	return lines
}

// Sending and polling are separate durable stages. No native RPC wait runs on
// the command ingress or the shared maintenance scheduler.
func (a *App) advanceTeamMCPReads(ctx context.Context, now time.Time) error {
	var pending []work.Operation
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var err error; pending, err = tx.ListPrivateMCPReads(); return err }); err != nil {
		return err
	}
	var failures []error
	for _, op := range pending {
		if err := a.advanceTeamMCPRead(ctx, op.ID, now); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (a *App) advanceTeamMCPRead(ctx context.Context, id string, now time.Time) error {
	var command agentproto.Command
	var instance string
	send := false
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		for _, step := range op.Steps {
			if step.ID == "ui_result" {
				return nil
			}
		}
		if op.State == work.OperationPrepared {
			body, err := tx.GetContent(op.ScopeID, op.RequestRef)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(body.Data, &command); err != nil {
				return err
			}
			worker, err := tx.GetWorker(op.TargetID)
			if err != nil {
				return err
			}
			if command.Execution == nil || worker.Generation != op.TargetGeneration || worker.NativeSessionID != command.Target.ThreadID {
				op.State, op.LastError = work.OperationTerminal, "执行会话已改变，请重新查询 MCP 服务。"
			} else {
				op.State, instance, send = work.OperationSending, worker.InstanceID, true
				op.Steps = append(op.Steps, work.OperationStep{ID: "dispatch_started", State: work.OperationConfirmed, Receipt: now.Format(time.RFC3339Nano)})
			}
			previous := op.Revision
			op.Revision++
			op.UpdatedAt = now
			_, _, err = tx.PutOperation(op, previous)
			return err
		}
		lines := []string{}
		if op.State == work.OperationConfirmed {
			event, err := tx.NativeMCPCommandResult(op.ID)
			if err != nil {
				return err
			}
			if event.RuntimeMCP == nil {
				return fmt.Errorf("MCP query receipt has no typed result")
			}
			lines = teamMCPReadLines(*event.RuntimeMCP)
		} else if op.State == work.OperationTerminal {
			lines = []string{op.LastError}
		} else if op.LastError != "" {
			lines = []string{"MCP 查询未完成：" + op.LastError}
		} else if teamMCPReadTimedOut(op, now) {
			previous := op.Revision
			op.State, op.LastError = work.OperationUnknown, "尚未收到 MCP 查询结果。可以刷新重新核对；本次查询没有启动业务执行。"
			op.Revision++
			if _, _, err = tx.PutOperation(op, previous); err != nil {
				return err
			}
			lines = []string{op.LastError}
		} else {
			return touchTeamPrivateNativeUI(tx, op, now)
		}
		return a.presentTeamPrivateNativeResult(tx, op, teamMCPReadPanel(lines), now, true)
	})
	if err != nil || !send {
		return err
	}
	if a.teamRuntime.driver == nil {
		err = fmt.Errorf("执行宿主尚未连接")
	} else {
		err = a.teamRuntime.driver.SendCommand(ctx, instance, command)
	}
	if err == nil {
		return nil
	}
	return a.teamRuntime.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.State == work.OperationConfirmed {
			return nil
		}
		previous := op.Revision
		op.Revision++
		op.State, op.LastError, op.UpdatedAt = work.OperationUnknown, err.Error(), now
		_, _, e = tx.PutOperation(op, previous)
		return e
	})
}

func teamMCPReadTimedOut(op work.Operation, now time.Time) bool {
	started := op.CreatedAt
	for _, step := range op.Steps {
		if step.ID == "dispatch_started" {
			if parsed, err := time.Parse(time.RFC3339Nano, step.Receipt); err == nil {
				started = parsed
			}
		}
	}
	return now.Sub(started) >= 30*time.Second
}
