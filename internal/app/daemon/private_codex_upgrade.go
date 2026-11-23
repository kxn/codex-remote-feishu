package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	codexui "github.com/kxn/codex-remote-feishu/internal/app/daemon/codexupgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

const privateCodexUpgradePrefix = "private-upgrade-codex-"

type privateCodexUpgradeReceipt struct {
	Version       int
	EffectID      string
	Before, After *orchestrator.PrivateUpgradeFlow
	Event         json.RawMessage
	Next          string
	Rejected      bool
}
type privateCodexUpgradeReducer struct{ host *App }

func (r privateCodexUpgradeReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	original, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var result privateCodexUpgradeReceipt
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	cmd := original.DaemonCommand
	if result.Rejected {
		if result.Version != 1 || result.EffectID != effect.ID || cmd == nil || result.Next != "" || result.After != nil || (cmd.Kind != control.DaemonCommandUpgrade && cmd.Kind != control.DaemonCommandUpgradeOwnerFlow) {
			return nil, privatechat.ErrJournalConflict
		}
		event, err := privatechat.DecodeEffectEvent(result.Event)
		if err != nil {
			return nil, err
		}
		if event.Kind != eventcontract.KindNotice || event.Notice == nil || event.Notice.Code != "codex_upgrade_owner_expired" || event.SurfaceSessionID != original.SurfaceSessionID || event.Command != nil || event.DaemonCommand != nil {
			return nil, privatechat.ErrJournalConflict
		}
		// Rejecting an old card does not overwrite a concurrently advancing flow.
		return []eventcontract.Event{event}, nil
	}
	if result.Version != 1 || result.EffectID != effect.ID || cmd == nil || result.After == nil || result.After.Source != "codex" || !strings.HasPrefix(result.After.ID, privateCodexUpgradePrefix) {
		return nil, privatechat.ErrJournalConflict
	}
	if !reflect.DeepEqual(app.Service().PrivateUpgradeFlow(original.SurfaceSessionID), result.Before) {
		return nil, privatechat.ErrJournalConflict
	}
	if cmd.Kind == control.DaemonCommandUpgrade {
		if result.After.ID != privateCodexUpgradePrefix+effect.ID {
			return nil, privatechat.ErrJournalConflict
		}
	} else if cmd.Kind != control.DaemonCommandUpgradeOwnerFlow || cmd.PickerID != result.After.ID {
		return nil, privatechat.ErrJournalConflict
	}
	event, err := privatechat.DecodeEffectEvent(result.Event)
	if err != nil {
		return nil, err
	}
	if event.SurfaceSessionID != original.SurfaceSessionID || event.Command != nil || event.DaemonCommand != nil {
		return nil, privatechat.ErrJournalConflict
	}
	if err = app.Service().SetPrivateUpgradeFlow(original.SurfaceSessionID, result.After); err != nil {
		return nil, err
	}
	out := []eventcontract.Event{event}
	if result.Next != "" {
		if result.Next != privateUpgradeExecute && result.Next != privateUpgradeResult {
			return nil, privatechat.ErrJournalConflict
		}
		next := control.DaemonCommand{Kind: control.DaemonCommandUpgradeOwnerFlow, GatewayID: cmd.GatewayID, SurfaceSessionID: cmd.SurfaceSessionID, SourceMessageID: result.After.MessageID, PickerID: result.After.ID, OptionID: result.Next}
		out = append(out, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, GatewayID: cmd.GatewayID, SurfaceSessionID: cmd.SurfaceSessionID, DaemonCommand: &next})
	}
	return out, nil
}

func privateCodexView(o privatechat.Owner, flow *orchestrator.PrivateUpgradeFlow) *codexui.OwnerCardFlowRecord {
	return &codexui.OwnerCardFlowRecord{FlowID: flow.ID, SurfaceSessionID: o.SurfaceID, OwnerUserID: o.UserID, MessageID: flow.MessageID, CurrentVersion: flow.CurrentVersion, LatestVersion: flow.TargetVersion, TargetVersion: flow.TargetVersion, Checked: flow.CodexChecked, HasUpdate: flow.TargetVersion != "" && flow.TargetVersion != flow.CurrentVersion, CanUpgrade: flow.CodexCanUpgrade, CreatedAt: flow.CreatedAt, UpdatedAt: time.Now().UTC(), ExpiresAt: flow.ExpiresAt}
}

func commitPrivateCodexView(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, before, after *orchestrator.PrivateUpgradeFlow, event eventcontract.Event, next string) ([]privatechat.EffectRecord, error) {
	event.GatewayID, event.SurfaceSessionID = o.GatewayID, o.SurfaceID
	if after.MessageID != "" {
		event.SourceMessageID, event.InlineReplaceCurrentCard = after.MessageID, true
		if event.PageView != nil {
			event.PageView.MessageID = after.MessageID
		}
	}
	eventRaw, err := privatechat.EncodeEffectEvent(event)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(privateCodexUpgradeReceipt{Version: 1, EffectID: record.Effect.ID, Before: before, After: after, Event: eventRaw, Next: next})
	if err != nil {
		return nil, err
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = j.ConfirmLocalEffect(commitCtx, o, generation, record.Effect.ID, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(commitCtx, o, generation, record.Effect.ID, privateCodexUpgradeReducer{host})
}

// Private cards retain the original open/check/confirm interaction. Installation
// belongs exclusively to the shared coordinator; this path never runs npm or
// restores carriers on its own.
func runPrivateCodexUpgrade(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, cmd control.DaemonCommand) ([]privatechat.EffectRecord, error) {
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, privateCodexUpgradeReducer{host})
	}
	if record.State != "prepared" && record.State != "unknown" && record.State != "sending" {
		return nil, privatechat.ErrJournalState
	}
	var before *orchestrator.PrivateUpgradeFlow
	if err := p.InspectPendingHostIntent(ctx, o, generation, record.Effect.ID, func(app *privatechat.App, _ privatechat.EffectRecord) error {
		before = app.Service().PrivateUpgradeFlow(o.SurfaceID)
		return nil
	}); err != nil {
		return nil, err
	}
	opening := cmd.Kind == control.DaemonCommandUpgrade
	if opening {
		if before != nil && (before.Phase == "running" || before.Phase == "restarting") {
			return rejectPrivateCodexCard(ctx, host, p, j, o, generation, record, "已有升级正在进行，请查看原升级卡片。")
		}
	} else if before == nil || before.Source != "codex" || before.ID != cmd.PickerID || before.OwnerUserID != o.UserID {
		return rejectPrivateCodexCard(ctx, host, p, j, o, generation, record, "这张 Codex 升级卡片已失效，请重新发送 /upgrade codex。")
	}
	if !opening && (cmd.OptionID == privateUpgradeExecute || cmd.OptionID == privateUpgradeResult) {
		return runPrivateCodexMaintenance(ctx, host, p, j, o, generation, record, cmd, before)
	}
	if !opening && (cmd.SourceMessageID == "" || before.MessageID != "" && before.MessageID != cmd.SourceMessageID || !before.ExpiresAt.After(time.Now())) {
		return rejectPrivateCodexCard(ctx, host, p, j, o, generation, record, "这张 Codex 升级卡片已失效，请重新发送 /upgrade codex。")
	}
	if !opening && !((cmd.OptionID == upgradeOwnerActionCheck || cmd.OptionID == upgradeOwnerActionCancel) && (before.Phase == "open" || before.Phase == "confirm") || cmd.OptionID == upgradeOwnerActionConfirm && before.Phase == "confirm" && before.CodexChecked && before.CodexCanUpgrade) {
		return rejectPrivateCodexCard(ctx, host, p, j, o, generation, record, "这项操作已经处理或不再可用，请查看当前升级卡片。")
	}
	if record.State == "prepared" {
		if err := j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	after := &orchestrator.PrivateUpgradeFlow{ID: privateCodexUpgradePrefix + record.Effect.ID, OwnerUserID: o.UserID, Source: "codex", Phase: "open", CreatedAt: now, ExpiresAt: now.Add(codexUpgradeOwnerFlowTTL)}
	if opening && cmd.FromCardAction {
		after.MessageID = cmd.SourceMessageID
	}
	if !opening {
		copy := *before
		after = &copy
		after.MessageID = cmd.SourceMessageID
	}
	var event eventcontract.Event
	next := ""
	switch {
	case opening:
		queryCtx, cancel := context.WithTimeout(ctx, codexUpgradeCheckTimeout)
		installation, err := host.inspectStandaloneCodexInstallation(queryCtx)
		cancel()
		if err == nil {
			after.CurrentVersion = installation.CurrentVersion()
		}
		event = codexUpgradeOwnerOpenEvent(o.SurfaceID, privateCodexView(o, after))
	case cmd.OptionID == upgradeOwnerActionCheck && (before.Phase == "open" || before.Phase == "confirm"):
		queryCtx, cancel := context.WithTimeout(ctx, codexUpgradeCheckTimeout)
		installation, err := host.inspectStandaloneCodexInstallation(queryCtx)
		latest := ""
		if err == nil && installation.Upgradeable() {
			lookup := host.codexUpgradeRuntime.LatestLookup
			if lookup == nil {
				lookup = func(ctx context.Context) (string, error) {
					return codexupgrade.LookupLatestVersion(ctx, codexupgrade.LatestVersionOptions{})
				}
			}
			latest, err = lookup(queryCtx)
			if err == nil && !codexupgrade.ValidTargetVersion(latest) {
				err = fmt.Errorf("版本来源没有返回有效的 Codex 版本")
			}
		}
		cancel()
		after.CodexCanUpgrade, after.CodexChecked = false, false
		after.StateFingerprint, after.TargetVersion = "", ""
		detail := ""
		if err != nil {
			detail = "检查失败：" + err.Error()
		} else {
			after.CurrentVersion = installation.CurrentVersion()
			if !installation.Upgradeable() {
				detail = installation.Problem
				if detail == "" {
					detail = "当前安装来源不支持自动升级，请通过原安装来源更新。"
				}
			} else {
				after.TargetVersion, after.CodexChecked = latest, true
				after.StateFingerprint = workupgrade.InstallationFingerprint(installation)
				after.CodexCanUpgrade = latest != after.CurrentVersion && host.teamRuntime != nil && host.teamRuntime.codexUpgrader != nil
				if !after.CodexCanUpgrade && latest != after.CurrentVersion {
					detail = "共享 Codex 维护服务尚未就绪，本次没有启动更新。"
				}
			}
		}
		after.Phase = "confirm"
		after.ExpiresAt = now.Add(codexUpgradeOwnerFlowTTL)
		event = codexUpgradeOwnerStableEvent(o.SurfaceID, privateCodexView(o, after), detail)
	case cmd.OptionID == upgradeOwnerActionConfirm && before.Phase == "confirm" && before.CodexChecked && before.CodexCanUpgrade:
		after.Phase, next = "running", privateUpgradeExecute
		event = codexUpgradeOwnerRunningEvent(o.SurfaceID, privateCodexView(o, after))
	case cmd.OptionID == upgradeOwnerActionCancel && (before.Phase == "open" || before.Phase == "confirm"):
		after.Phase = "cancelled"
		event = codexUpgradeOwnerTerminalEvent(o.SurfaceID, privateCodexView(o, after), "Codex 升级已取消", "info", "本次没有更新程序。")
	default:
		return nil, privatechat.ErrJournalConflict
	}
	return commitPrivateCodexView(ctx, host, p, j, o, generation, record, before, after, event, next)
}

type privateCodexCoordinator interface {
	RequestPrivate(context.Context, workupgrade.PrivateRequest, workupgrade.PrivateRequestAuthority) (workupgrade.Record, error)
	Status(context.Context, string) (workupgrade.Record, error)
}

func runPrivateCodexMaintenance(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, cmd control.DaemonCommand, before *orchestrator.PrivateUpgradeFlow) ([]privatechat.EffectRecord, error) {
	if host.teamRuntime == nil {
		return nil, privatechat.ErrDeferred
	}
	coordinator, ok := host.teamRuntime.codexUpgrader.(privateCodexCoordinator)
	if !ok {
		return nil, privatechat.ErrDeferred
	}
	if cmd.OptionID == privateUpgradeExecute && before.Phase != "running" || cmd.OptionID == privateUpgradeResult && before.Phase != "restarting" {
		return nil, privatechat.ErrJournalConflict
	}

	var status workupgrade.Record
	var err error
	if cmd.OptionID == privateUpgradeResult {
		status, err = coordinator.Status(ctx, before.SharedRequestID)
		if err != nil {
			return nil, err
		}
		if status.Stage != "completed" && codexMaintenanceDisplayHash(status) == before.SharedDisplayHash {
			return nil, privatechat.ErrDeferred
		}
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
			return nil, err
		}
	}
	after := *before
	next, title, theme, detail := "", "Codex 升级完成", "success", "受管 Codex 已恢复，未处理的输入将按原设置继续。"
	if cmd.OptionID == privateUpgradeExecute {
		source := workstore.NativeUpgradePrivateSource{Tenant: o.TenantID, AppID: o.AppID, Gateway: o.GatewayID, Surface: o.SurfaceID, Chat: o.ChatID, User: o.UserID, Generation: generation, EffectID: record.Effect.ID}
		req := workupgrade.PrivateRequest{ID: "private-codex:" + record.Effect.ID, TargetVersion: before.TargetVersion, InstallationFingerprint: before.StateFingerprint, Source: source}
		status, err = coordinator.RequestPrivate(ctx, req, privateCodexUpgradeAuthority{p})
		if err != nil {
			// A failed call is not proof that the other database did not commit.
			var lookupErr error
			status, lookupErr = coordinator.Status(context.WithoutCancel(ctx), req.ID)
			if lookupErr != nil && !errors.Is(lookupErr, workstore.ErrNotFound) {
				_ = j.ResolveEffect(context.WithoutCancel(ctx), o, generation, record.Effect.ID, "unknown", []byte(`{"shared_request":"unresolved"}`), nil)
				return nil, privatechat.ErrDeferred
			}
			if errors.Is(lookupErr, workstore.ErrNotFound) {
				after.Phase, title, theme, detail = "failed", "Codex 升级未开始", "error", err.Error()+"。请重新打开 /upgrade codex 检查。"
			}
		}
		if status.ID != "" {
			if status.ID != req.ID || status.PrivateSource == nil || *status.PrivateSource != source || status.TargetVersion != req.TargetVersion || status.InstallationFingerprint != req.InstallationFingerprint {
				return nil, privatechat.ErrJournalConflict
			}
			after.SharedRequestID, after.Phase, next = status.ID, "restarting", privateUpgradeResult
			after.SharedDisplayHash = codexMaintenanceDisplayHash(status)
			event := codexUpgradeOwnerRunningEvent(o.SurfaceID, privateCodexView(o, &after))
			return commitPrivateCodexView(ctx, host, p, j, o, generation, record, before, &after, event, next)
		}
	} else {
		if status.ID != before.SharedRequestID || status.PrivateSource == nil || status.ID != "private-codex:"+status.PrivateSource.EffectID || status.PrivateSource.Surface != o.SurfaceID || status.PrivateSource.Tenant != o.TenantID || status.PrivateSource.AppID != o.AppID || status.PrivateSource.Gateway != o.GatewayID || status.PrivateSource.Chat != o.ChatID || status.PrivateSource.User != o.UserID || status.PrivateSource.Generation != generation || status.TargetVersion != before.TargetVersion || status.InstallationFingerprint != before.StateFingerprint {
			return nil, privatechat.ErrJournalConflict
		}
		after.SharedDisplayHash = codexMaintenanceDisplayHash(status)
		if status.Stage != "completed" {
			panel := teamCodexUpgradePanel(status)
			event := codexUpgradeOwnerCardEvent(o.SurfaceID, privateCodexView(o, &after), "Codex 程序维护", "progress", codexUpgradeOwnerContextSections(privateCodexView(o, &after)), codexUpgradeOwnerNoticeSections(panel.Paragraphs...), nil, false)
			return commitPrivateCodexView(ctx, host, p, j, o, generation, record, before, &after, event, privateUpgradeResult)
		}
		after.Phase = "completed"
	}
	event := codexUpgradeOwnerTerminalEvent(o.SurfaceID, privateCodexView(o, &after), title, theme, detail)
	return commitPrivateCodexView(ctx, host, p, j, o, generation, record, before, &after, event, next)
}

// Polling or repeated identical recovery errors do not generate new cards.
func codexMaintenanceDisplayHash(record workupgrade.Record) string {
	raw, _ := json.Marshal([]string{record.Stage, record.ManagedVersion, record.LastError})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
