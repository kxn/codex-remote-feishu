package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	upgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/upgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type privateUpgradeCheckReceipt struct {
	Version  int
	EffectID string
	Flow     *orchestrator.PrivateUpgradeFlow
	Event    json.RawMessage
	Execute  bool
}
type privateUpgradeCheckReducer struct{ host *App }

func (r privateUpgradeCheckReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	original, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var result privateUpgradeCheckReceipt
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result.Version != 1 || result.EffectID != effect.ID || original.DaemonCommand == nil || original.DaemonCommand.Kind != control.DaemonCommandUpgrade {
		return nil, privatechat.ErrJournalConflict
	}
	if result.Flow != nil {
		if result.Flow.ID != "private-upgrade-"+effect.ID {
			return nil, privatechat.ErrJournalConflict
		}
		if err = app.Service().SetPrivateUpgradeFlow(original.SurfaceSessionID, result.Flow); err != nil {
			return nil, err
		}
	}
	event, err := privatechat.DecodeEffectEvent(result.Event)
	if err != nil {
		return nil, err
	}
	if event.SurfaceSessionID != original.SurfaceSessionID || event.Command != nil || event.DaemonCommand != nil {
		return nil, privatechat.ErrJournalConflict
	}
	if original.DaemonCommand.FromCardAction && original.DaemonCommand.SourceMessageID != "" {
		event.SourceMessageID = original.DaemonCommand.SourceMessageID
		event.InlineReplaceCurrentCard = true
		if event.PageView != nil {
			event.PageView.MessageID = event.SourceMessageID
		}
	}
	out := []eventcontract.Event{event}
	if result.Execute {
		if result.Flow == nil || result.Flow.Source != string(install.UpgradeSourceLocal) || result.Flow.Phase != "running" {
			return nil, privatechat.ErrJournalConflict
		}
		cmd := original.DaemonCommand
		next := control.DaemonCommand{Kind: control.DaemonCommandUpgradeOwnerFlow, GatewayID: cmd.GatewayID, SurfaceSessionID: cmd.SurfaceSessionID, SourceMessageID: cmd.SourceMessageID, PickerID: result.Flow.ID, OptionID: privateUpgradeExecute}
		out = append(out, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, GatewayID: cmd.GatewayID, SurfaceSessionID: cmd.SurfaceSessionID, DaemonCommand: &next})
	}
	return out, nil
}

// The check is read-only outside the shared upgrade owner lock. The result and
// owner card are frozen in a durable receipt before private core mutation.
func runPrivateUpgradeLatest(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, cmd control.DaemonCommand) ([]privatechat.EffectRecord, error) {
	parsed, parseErr := parseUpgradeCommandText(cmd.Text)
	if parseErr != nil || (parsed.Mode != upgradeCommandLatest && parsed.Mode != upgradeCommandDev) {
		return nil, privatechat.ErrJournalConflict
	}
	dev := parsed.Mode == upgradeCommandDev
	reducer := privateUpgradeCheckReducer{host}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, privatechat.ErrJournalState
	}
	if record.State == "prepared" {
		if err := j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
			return nil, err
		}
	}
	host.mu.Lock()
	state, _, err := host.loadUpgradeStateLocked(true)
	if err != nil {
		host.mu.Unlock()
		return retryPrivateUpgradeCheck(ctx, j, o, generation, record, err)
	}
	if host.upgradeRuntime.CheckInFlight || host.upgradeRuntime.StartInFlight || pendingUpgradeBusy(state.PendingUpgrade) {
		host.mu.Unlock()
		return retryPrivateUpgradeCheck(ctx, j, o, generation, record, fmt.Errorf("upgrade transaction already active"))
	}
	host.upgradeRuntime.CheckInFlight = true
	lookup := host.upgradeRuntime.Lookup
	if lookup == nil {
		lookup = host.defaultReleaseLookup
	}
	devLookup := host.upgradeRuntime.DevManifest
	if devLookup == nil {
		devLookup = host.defaultDevManifestLookup
	}
	track := state.CurrentTrack
	if track == "" {
		track = defaultUpgradeTrackForState(state)
	}
	host.mu.Unlock()
	targetVersion := ""
	source := install.UpgradeSourceRelease
	var lookupErr error
	if dev {
		source = install.UpgradeSourceDev
		if !install.CurrentBuildAllowsDevUpgrade() {
			lookupErr = fmt.Errorf("当前构建不支持 dev 构建升级")
		} else {
			var manifest install.DevManifest
			manifest, _, lookupErr = devLookup(ctx)
			targetVersion = manifest.Version
		}
	} else {
		var release install.ReleaseInfo
		release, lookupErr = lookup(ctx, track)
		targetVersion = release.TagName
	}
	host.mu.Lock()
	host.upgradeRuntime.CheckInFlight = false
	host.mu.Unlock()
	now := time.Now().UTC()
	result := privateUpgradeCheckReceipt{Version: 1, EffectID: record.Effect.ID}
	var event eventcontract.Event
	if lookupErr != nil || strings.TrimSpace(targetVersion) == "" {
		detail := "升级来源未返回版本"
		if lookupErr != nil {
			detail = lookupErr.Error()
		}
		event = upgradeNoticeEvent(o.SurfaceID, "upgrade_check_failed", fmt.Sprintf("检查更新失败：%s", detail))
	} else if strings.TrimSpace(targetVersion) == strings.TrimSpace(state.CurrentVersion) {
		event = upgradeNoticeEvent(o.SurfaceID, "upgrade_latest", fmt.Sprintf("当前已经是 %s track 的最新版本 %s。", track, targetVersion))
	} else {
		messageID := ""
		if cmd.FromCardAction {
			messageID = cmd.SourceMessageID
		}
		flow := &orchestrator.PrivateUpgradeFlow{ID: "private-upgrade-" + record.Effect.ID, OwnerUserID: o.UserID, MessageID: messageID, Phase: "confirm", Source: string(source), Track: string(track), CurrentVersion: state.CurrentVersion, TargetVersion: targetVersion, StateFingerprint: install.UpgradeStateFingerprint(state), CreatedAt: now, ExpiresAt: now.Add(upgradeOwnerFlowTTL)}
		result.Flow = flow
		view := &upgraderuntime.OwnerCardFlowRecord{FlowID: flow.ID, SurfaceSessionID: o.SurfaceID, OwnerUserID: o.UserID, MessageID: messageID, Stage: upgraderuntime.OwnerFlowStageConfirm, Source: source, Track: track, CurrentVersion: state.CurrentVersion, TargetVersion: targetVersion, CreatedAt: now, UpdatedAt: now, ExpiresAt: flow.ExpiresAt}
		event = upgradeOwnerConfirmEvent(o.SurfaceID, view, state)
	}
	event.GatewayID = o.GatewayID
	event.SurfaceSessionID = o.SurfaceID
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
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
}

func retryPrivateUpgradeCheck(ctx context.Context, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, cause error) ([]privatechat.EffectRecord, error) {
	if record.State == "prepared" {
		raw, _ := json.Marshal(map[string]string{"error": cause.Error()})
		if err := j.ResolveEffect(context.WithoutCancel(ctx), o, generation, record.Effect.ID, "unknown", raw, nil); err != nil {
			return nil, err
		}
	}
	return nil, privatechat.ErrDeferred
}
