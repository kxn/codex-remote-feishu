package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"net/url"
	"sort"
	"strings"
	"time"
)

func (a *App) handleTeamMCP(ctx context.Context, source teamHumanSource, action control.Action) (string, error) {
	args := strings.Fields(action.Text)
	if len(args) > 0 {
		args[0] = strings.ToLower(args[0])
	}
	verb := "status"
	if len(args) > 1 {
		verb = strings.ToLower(args[1])
	}
	if len(args) > 0 && (args[0] == "/mcpoauth" || args[0] == "/mcp-oauth") {
		if len(args) == 2 && !strings.EqualFold(args[1], "status") {
			args = []string{"/mcp", "login", args[1]}
			verb = "login"
		}
	}
	if verb == "login" {
		if len(args) != 3 {
			return "", fmt.Errorf("用法：/mcp login <服务名> 或 /mcpoauth <服务名>")
		}
		return a.startTeamMCPOAuth(ctx, source, args[2])
	}
	if verb == "oauth-status" {
		if len(args) != 2 {
			return "", fmt.Errorf("用法：/mcp oauth-status")
		}
		return a.queueTeamMCPOAuthStatus(ctx, source)
	}
	if verb != "status" && verb != "config" && verb != "configuration" {
		return "", fmt.Errorf("用法：/mcp status、/mcp config、/mcp login <服务名> 或 /mcp oauth-status")
	}
	cursor := ""
	if len(args) > 2 {
		if len(args) != 4 || args[2] != "--cursor" {
			return "", fmt.Errorf("分页用法：/mcp status --cursor <游标>")
		}
		cursor = args[3]
	}
	mode := "status"
	if verb != "status" {
		mode = "configuration"
	}
	return a.queueTeamMCPRead(ctx, source, agentproto.RuntimeMCPQuery{Version: agentproto.RuntimeMCPProtocolVersion, Mode: mode, Cursor: cursor, Limit: 100})
}
func (a *App) startTeamMCPOAuth(ctx context.Context, source teamHumanSource, server string) (string, error) {
	if server == "" || len(server) > 256 {
		return "", fmt.Errorf("请填写 MCP 服务名")
	}
	var command agentproto.Command
	var instance string
	send := false
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, carrier, e := a.teamNativeTarget(tx, source)
		if e != nil {
			return e
		}
		if carrier.Owner.BotGeneration != source.Gateway.BotGeneration {
			return fmt.Errorf("身份配置已更新，请先核对并重建当前执行环境")
		}
		if worker.Backend != work.BackendCodex {
			return fmt.Errorf("当前后端没有提供原生 MCP OAuth 登录接口；可以查询服务状态，并通过该服务的登录方式完成认证")
		}
		_, human, e := tx.ValidatePrivateAction(source.Authority)
		if e != nil {
			return e
		}
		id := teamID("user-mcp-oauth", source.Key)
		if old, e := tx.GetOperation(id); e == nil {
			if old.TargetID != worker.ID {
				return workstore.ErrIdempotencyConflict
			}
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		binding := carrier.Owner
		command = agentproto.Command{CommandID: id, IssuedAt: time.Now().UTC(), Kind: agentproto.CommandMCPOAuthLogin, Execution: &binding, Target: agentproto.Target{ThreadID: worker.NativeSessionID}, MCP: agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: server, ThreadID: worker.NativeSessionID, TimeoutSecs: 300}}}
		raw, e := json.Marshal(command)
		if e != nil {
			return e
		}
		body := work.NewContent(id+":body", worker.ScopeID, "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: id, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "mcp-user"}, Kind: string(command.Kind), TargetID: worker.ID, TargetGeneration: worker.Generation, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: id, Steps: []work.OperationStep{{ID: "human_source", State: work.OperationConfirmed, Receipt: stringMustJSON(human)}}, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, 0)
		instance = worker.InstanceID
		send = e == nil
		return e
	})
	if err != nil {
		return "", err
	}
	if send {
		if err = a.teamRuntime.driver.SendCommand(ctx, instance, command); err != nil {
			_ = a.teamRuntime.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(command.CommandID)
				if e != nil {
					return e
				}
				before := op.Revision
				op.Revision++
				op.State = work.OperationUnknown
				op.LastError = err.Error()
				_, _, e = tx.PutOperation(op, before)
				return e
			})
			return "", err
		}
	}
	return a.queueTeamMCPOAuthStatus(ctx, source)
}
func (a *App) queueTeamMCPOAuthStatus(ctx context.Context, source teamHumanSource) (string, error) {
	panel := workui.Panel{Title: "MCP 服务认证"}
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		wc, _, e := worksettings.FrozenContext(tx, source.Authority)
		if e != nil {
			return e
		}
		operations, e := tx.ListOperations(wc.ScopeID)
		if e != nil {
			return e
		}
		sort.Slice(operations, func(i, j int) bool { return operations[i].CreatedAt.After(operations[j].CreatedAt) })
		shown := 0
		for _, op := range operations {
			if op.ContextID != wc.ID || op.Kind != string(agentproto.CommandMCPOAuthLogin) || op.Actor != (work.Actor{Kind: work.ActorSystem, ID: "mcp-user"}) {
				continue
			}
			event, e := tx.NativeMCPCommandResult(op.ID)
			if e == workstore.ErrNotFound {
				panel.Paragraphs = append(panel.Paragraphs, "认证请求已记录，正在等待授权链接。稍后用 /mcp oauth-status 查看。")
				shown++
				if shown == 5 {
					break
				}
				continue
			}
			if e != nil {
				return e
			}
			if event.MCPOAuthLogin == nil {
				return workstore.ErrScopeDenied
			}
			result := event.MCPOAuthLogin
			if event.Kind == agentproto.EventMCPOAuthLoginCompleted {
				if result.Success {
					panel.Paragraphs = append(panel.Paragraphs, result.ServerName+"：认证已完成。")
				} else {
					panel.Paragraphs = append(panel.Paragraphs, result.ServerName+"：认证未完成。"+result.Error)
				}
			} else {
				parsed, e := url.Parse(result.AuthorizationURL)
				if e != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
					return fmt.Errorf("服务返回了无法打开的授权链接")
				}
				panel.Paragraphs = append(panel.Paragraphs, result.ServerName+"：请打开授权链接完成认证：\n"+parsed.String()+"\n完成后用 /mcp oauth-status 核对结果。")
			}
			shown++
			if shown == 5 {
				break
			}
		}
		if len(panel.Paragraphs) == 0 {
			panel.Paragraphs = append(panel.Paragraphs, "还没有认证请求。用 /mcpoauth <服务名> 开始。")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return a.queueTeamPicker(ctx, source, "private.mcp-auth", source.ScopeID, source.NavigationRevision, panel)
}
