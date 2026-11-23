package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	upgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/upgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

const privateUpgradeExecute = "private-execute"

type privateUpgradeTransitionReceipt struct {
	Version       int
	EffectID      string
	Before, After *orchestrator.PrivateUpgradeFlow
	Event         json.RawMessage
	Execute       bool
	Rejected      bool
}
type privateUpgradeTransitionReducer struct{ host *App }

func (r privateUpgradeTransitionReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	original, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var result privateUpgradeTransitionReceipt
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	cmd := original.DaemonCommand
	if result.Version != 1 || result.EffectID != effect.ID || cmd == nil || cmd.Kind != control.DaemonCommandUpgradeOwnerFlow {
		return nil, privatechat.ErrJournalConflict
	}
	if result.Rejected {
		if result.Before != nil || result.After != nil || result.Execute || (cmd.OptionID != upgradeOwnerActionConfirm && cmd.OptionID != upgradeOwnerActionCancel) {
			return nil, privatechat.ErrJournalConflict
		}
		event, err := privatechat.DecodeEffectEvent(result.Event)
		if err != nil || event.SurfaceSessionID != original.SurfaceSessionID || event.Notice == nil || event.Notice.Code != "upgrade_owner_expired" || event.Command != nil || event.DaemonCommand != nil {
			return nil, privatechat.ErrJournalConflict
		}
		return []eventcontract.Event{event}, nil
	}
	if result.Before == nil {
		return nil, privatechat.ErrJournalConflict
	}
	current := app.Service().PrivateUpgradeFlow(original.SurfaceSessionID)
	if !reflect.DeepEqual(current, result.Before) {
		// An execution may reach an exact terminal before a concurrent cancel
		// receipt projects. Do not overwrite that terminal or poison the core.
		if cmd.OptionID == upgradeOwnerActionCancel && current != nil && current.ID == result.Before.ID && (current.Phase == "failed" || current.Phase == "cancelled" || current.Phase == "restarting") {
			return nil, nil
		}
		return nil, privatechat.ErrJournalConflict
	}
	if err = app.Service().SetPrivateUpgradeFlow(original.SurfaceSessionID, result.After); err != nil {
		return nil, err
	}
	event, err := privatechat.DecodeEffectEvent(result.Event)
	if err != nil {
		return nil, err
	}
	if event.SurfaceSessionID != original.SurfaceSessionID || event.Command != nil || event.DaemonCommand != nil {
		return nil, privatechat.ErrJournalConflict
	}
	out := []eventcontract.Event{event}
	if result.Execute {
		next := control.DaemonCommand{Kind: control.DaemonCommandUpgradeOwnerFlow, GatewayID: cmd.GatewayID, SurfaceSessionID: cmd.SurfaceSessionID, SourceMessageID: cmd.SourceMessageID, PickerID: cmd.PickerID, OptionID: privateUpgradeExecute}
		out = append(out, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, GatewayID: cmd.GatewayID, SurfaceSessionID: cmd.SurfaceSessionID, DaemonCommand: &next})
	}
	return out, nil
}
func upgradePrivateOwnerView(o privatechat.Owner, flow *orchestrator.PrivateUpgradeFlow) *upgraderuntime.OwnerCardFlowRecord {
	return &upgraderuntime.OwnerCardFlowRecord{FlowID: flow.ID, SurfaceSessionID: o.SurfaceID, OwnerUserID: o.UserID, MessageID: flow.MessageID, Stage: upgraderuntime.OwnerFlowStageConfirm, Source: install.UpgradeSource(flow.Source), Track: install.ReleaseTrack(flow.Track), CurrentVersion: flow.CurrentVersion, TargetVersion: flow.TargetVersion, CreatedAt: flow.CreatedAt, UpdatedAt: time.Now().UTC(), ExpiresAt: flow.ExpiresAt}
}
func runPrivateUpgradeTransition(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, cmd control.DaemonCommand) ([]privatechat.EffectRecord, error) {
	reducer := privateUpgradeTransitionReducer{host}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, privatechat.ErrJournalState
	}
	var flow *orchestrator.PrivateUpgradeFlow
	err := p.InspectLocalObservation(ctx, o, generation, record.Effect.ID, func(app *privatechat.App, _ privatechat.EffectRecord) error {
		flow = app.Service().PrivateUpgradeFlow(o.SurfaceID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if cmd.OptionID != upgradeOwnerActionConfirm && cmd.OptionID != upgradeOwnerActionCancel {
		return nil, privatechat.ErrJournalConflict
	}
	if flow == nil || flow.ID != cmd.PickerID || flow.OwnerUserID != o.UserID || !flow.ExpiresAt.After(time.Now()) || (flow.Phase != "confirm" && !(flow.Phase == "running" && cmd.OptionID == upgradeOwnerActionCancel)) || cmd.SourceMessageID == "" || (flow.MessageID != "" && flow.MessageID != cmd.SourceMessageID) {
		if record.State == "prepared" {
			if err = j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
				return nil, err
			}
		}
		event := upgradeNoticeEvent(o.SurfaceID, "upgrade_owner_expired", "这张升级卡片已失效，请重新发送 `/upgrade latest`。")
		event.GatewayID, event.SurfaceSessionID = o.GatewayID, o.SurfaceID
		encoded, e := privatechat.EncodeEffectEvent(event)
		if e != nil {
			return nil, e
		}
		raw, e := json.Marshal(privateUpgradeTransitionReceipt{Version: 1, EffectID: record.Effect.ID, Event: encoded, Rejected: true})
		if e != nil {
			return nil, e
		}
		if e = j.ConfirmLocalEffect(context.WithoutCancel(ctx), o, generation, record.Effect.ID, raw, privateLocalVerifier{o, generation, record.Effect, raw}); e != nil {
			return nil, e
		}
		return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
			return nil, err
		}
	}
	if flow.Phase == "running" && cmd.OptionID == upgradeOwnerActionCancel {
		if err = j.RecordUpgradeCancellation(ctx, o, generation, flow.ID, record.Effect.ID); err != nil {
			return nil, err
		}
		host.mu.Lock()
		cancel := host.upgradeRuntime.StartCancel
		if host.upgradeRuntime.StartFlowID != flow.ID {
			cancel = nil
		}
		host.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	view := upgradePrivateOwnerView(o, flow)
	view.MessageID = cmd.SourceMessageID
	result := privateUpgradeTransitionReceipt{Version: 1, EffectID: record.Effect.ID, Before: flow}
	var event eventcontract.Event
	if cmd.OptionID == upgradeOwnerActionConfirm {
		updated := *flow
		updated.MessageID = cmd.SourceMessageID
		updated.Phase = "running"
		result.After = &updated
		result.Execute = true
		event = upgradeOwnerRunningEvent(o.SurfaceID, view, "正在准备升级", "正在准备目标版本并校验升级事务。", true)
	} else if flow.Phase == "running" {
		updated := *flow
		updated.Phase = "cancelling"
		result.After = &updated
		event = upgradeOwnerRunningEvent(o.SurfaceID, view, "正在取消升级", "正在停止当前升级准备流程，请稍候。", false)
	} else {
		event = upgradeOwnerTerminalEvent(o.SurfaceID, view, "升级已取消", "info", upgradeOwnerContextSections(flow.CurrentVersion, flow.TargetVersion, flow.Track), upgradeOwnerNoticeSections("已取消这次升级。"))
	}
	event.GatewayID = o.GatewayID
	event.SurfaceSessionID = o.SurfaceID
	event.SourceMessageID = cmd.SourceMessageID
	event.InlineReplaceCurrentCard = true
	result.Event, err = privatechat.EncodeEffectEvent(event)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = j.ConfirmLocalEffect(commitCtx, o, generation, record.Effect.ID, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, fmt.Errorf("upgrade transition: %w", err)
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
}
