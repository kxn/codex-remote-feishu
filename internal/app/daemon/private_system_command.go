package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type privateSystemReceipt struct {
	Version  int
	EffectID string
	Command  control.DaemonCommand
	Events   []json.RawMessage
}

// System settings retain the old host implementation, but cannot write the
// conversation core or publish from an untracked goroutine. Their real result
// is checkpointed first and then projected through the ordinary private sender.
func runPrivateSystemCommand(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || (cmd.Kind != control.DaemonCommandAdmin && cmd.Kind != control.DaemonCommandDebug && cmd.Kind != control.DaemonCommandUpgrade) || event.SurfaceSessionID != o.SurfaceID || cmd.SurfaceSessionID != o.SurfaceID {
		return nil, privatechat.ErrJournalConflict
	}
	if cmd.Kind == control.DaemonCommandUpgrade {
		parsed, parseErr := parseUpgradeCommandText(cmd.Text)
		if parseErr == nil && parsed.Mode != upgradeCommandShowStatus && parsed.Mode != upgradeCommandShowTrack && parsed.Mode != upgradeCommandSetTrack {
			return nil, privatechat.ErrDeferred
		}
	}
	reducer := privateSystemReducer{host: host}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, reducer)
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, privatechat.ErrJournalState
	}
	recovering := record.State == "unknown"
	if !recovering {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	events := host.executePrivateSystemCommand(ctx, *cmd, recovering)
	result := privateSystemReceipt{Version: 1, EffectID: id, Command: *cmd}
	for _, next := range events {
		if next.Command != nil || next.DaemonCommand != nil {
			return nil, fmt.Errorf("system command returned an unowned effect")
		}
		next.GatewayID, next.SurfaceSessionID = o.GatewayID, o.SurfaceID
		raw, e := privatechat.EncodeEffectEvent(next)
		if e != nil {
			return nil, e
		}
		result.Events = append(result.Events, raw)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = j.ConfirmLocalEffect(commitCtx, o, generation, id, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, reducer)
}

func (a *App) executePrivateSystemCommand(ctx context.Context, cmd control.DaemonCommand, recovering bool) []eventcontract.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cmd.Kind == control.DaemonCommandDebug {
		return a.handleDebugDaemonCommand(cmd)
	}
	if cmd.Kind == control.DaemonCommandUpgrade {
		return a.handleUpgradeDaemonCommand(cmd)
	}
	parsed, err := parseAdminCommandText(cmd.Text)
	if err != nil {
		return adminUsageEvents(cmd.SurfaceSessionID, err.Error())
	}
	if recovering {
		switch parsed.Mode {
		case adminCommandNetworkSet:
			current := a.externalAccessNetworkModeLocked()
			message := "已重新读取当前网络模式。上次设置的执行结果未能完整保存，请按当前状态确认。"
			return commandPageEvents(cmd.SurfaceSessionID, buildAdminNetworkPageView(current, message))
		case adminCommandAutostartOn, adminCommandAutostartOff:
			status, e := detectAutostart(a.installStatePath())
			if e != nil {
				return adminNoticePageEvents(cmd.SurfaceSessionID, "自动启动", fmt.Sprintf("恢复时读取自动启动状态失败：%v", e))
			}
			return commandPageEvents(cmd.SurfaceSessionID, buildAdminAutostartPageView(status, "info", "已重新读取实际自动启动状态；没有重复执行上次设置。"))
		case adminCommandWeb:
			if a.externalAccessNetworkModeLocked() != "local" {
				return adminNoticePageEvents(cmd.SurfaceSessionID, "管理页链接", "上次链接生成过程被中断，无法确认结果。请再次打开管理页入口生成新链接。")
			}
		}
	}
	if parsed.Mode != adminCommandWeb || a.externalAccessNetworkModeLocked() == "local" {
		return a.handleAdminDaemonCommand(cmd)
	}
	service, localURL, err := a.ensureExternalAccessIssueTargetLocked()
	if err != nil {
		return adminNoticePageEvents(cmd.SurfaceSessionID, "管理页链接", fmt.Sprintf("生成管理页链接失败：%v", err))
	}
	request := debugAdminIssueRequest(a.admin.adminURL)
	// Reuse the real access service; its result returns to this durable effect,
	// never the legacy daemon's asynchronous UI queue.
	a.mu.Unlock()
	issueCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	issued, err := service.IssueURL(issueCtx, request, localURL)
	cancel()
	a.mu.Lock()
	if err != nil {
		return adminNoticePageEvents(cmd.SurfaceSessionID, "管理页链接", fmt.Sprintf("生成管理页链接失败：%v", err))
	}
	text := fmt.Sprintf("[打开管理页](%s)\n\n有效期到：`%s`", issued.ExternalURL, issued.ExpiresAt.UTC().Format(time.RFC3339))
	return adminNoticePageEvents(cmd.SurfaceSessionID, "管理页链接", text)
}

type privateSystemReducer struct{ host *App }

func (r privateSystemReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	original, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var result privateSystemReceipt
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	a, _ := json.Marshal(original.DaemonCommand)
	b, _ := json.Marshal(result.Command)
	if result.Version != 1 || result.EffectID != effect.ID || string(a) != string(b) {
		return nil, privatechat.ErrJournalConflict
	}
	var out []eventcontract.Event
	for i, body := range result.Events {
		event, e := privatechat.DecodeEffectEvent(body)
		if e != nil {
			return nil, e
		}
		if event.SurfaceSessionID != original.SurfaceSessionID || event.Command != nil || event.DaemonCommand != nil {
			return nil, privatechat.ErrJournalConflict
		}
		if i == 0 && result.Command.FromCardAction && result.Command.SourceMessageID != "" {
			event.SourceMessageID = result.Command.SourceMessageID
			event.InlineReplaceCurrentCard = true
			if event.PageView != nil {
				event.PageView.MessageID = result.Command.SourceMessageID
			}
		}
		out = append(out, event)
	}
	return out, nil
}
