package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type privateSettingsIntent struct {
	Version                                            int
	GatewayID, AppID, ActorID, Expected, CardMessageID string
	Requested                                          state.BotCapabilitySettingsRecord
	Events                                             []json.RawMessage
	Transition                                         *control.Action
}
type privateSettingsReceipt struct {
	Version     int
	EffectID    string
	Record      state.BotCapabilitySettingsRecord
	Fingerprint string
	Revision    uint64
	Applied     bool
}

func (r privateHostReducer) reduceSettingsTransitionIntent(app *privatechat.App, action control.Action) ([]eventcontract.Event, error) {
	value, ok := r.host.privateSettingsCache.Load(action.GatewayID)
	if !ok {
		return nil, privatechat.ErrDeferred
	}
	entry := value.(privateBotSettingsCacheEntry)
	owner, owned := app.OwnerForSurface(action.SurfaceSessionID)
	if !owned || entry.Owner.TenantID != owner.TenantID || entry.Owner.AppID != owner.AppID || entry.Owner.GatewayID != owner.GatewayID || entry.Snapshot.Revision != r.host.privateSettingsRevision.Load() {
		return nil, privatechat.ErrDeferred
	}
	proposal, events, err := app.Service().PreviewPrivateSettingsTransition(action)
	if err != nil {
		return nil, err
	}
	if proposal == nil {
		return events, nil
	}
	intent := privateSettingsIntent{Version: 1, GatewayID: action.GatewayID, AppID: owner.AppID, ActorID: action.ActorUserID, Expected: entry.Snapshot.Fingerprint, Requested: *proposal, Transition: &action}
	if action.Inbound != nil && action.Inbound.CardCallback {
		intent.CardMessageID = action.Inbound.OpenMessageID
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	return []eventcontract.Event{{Kind: eventcontract.KindDaemonCommand, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandPrivateBotSettings, SurfaceSessionID: action.SurfaceSessionID, Text: string(raw)}}}, nil
}

// runPrivateBotSettings consumes one committed config intent. Unknown writes
// are reconciled against the same authoritative owner, not blindly replayed.
func runPrivateBotSettings(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateBotSettings || event.SurfaceSessionID != o.SurfaceID || cmd.SurfaceSessionID != o.SurfaceID {
		return nil, privatechat.ErrJournalConflict
	}
	var intent privateSettingsIntent
	if err = json.Unmarshal([]byte(cmd.Text), &intent); err != nil {
		return nil, err
	}
	if intent.Version != 1 || intent.GatewayID != o.GatewayID || intent.AppID != o.AppID || intent.ActorID != o.UserID || intent.Requested.GatewayID != o.GatewayID || intent.Expected == "" {
		return nil, privatechat.ErrJournalConflict
	}
	if record.State == "confirmed" {
		if err := host.refreshPrivateBotSettings(ctx, p, o, generation); err != nil {
			return nil, err
		}
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateSettingsReducer{host})
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, fmt.Errorf("bot settings result requires reconciliation")
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	snapshot, err := host.compareAndSwapPrivateBotSettings(o.GatewayID, o.AppID, o.UserID, intent.Expected, intent.Requested)
	if err != nil {
		if errors.Is(err, errPrivateBotSettingsConflict) {
			snapshot, err = host.readPrivateBotSettings(o.GatewayID, o.AppID)
			if err == nil {
				return completePrivateBotSettings(ctx, host, p, j, o, generation, record, snapshot, false)
			}
		}
		if record.State == "prepared" {
			raw, _ := json.Marshal(map[string]string{"error": err.Error()})
			_ = j.ResolveEffect(context.WithoutCancel(ctx), o, generation, id, "unknown", raw, nil)
		}
		return nil, err
	}
	return completePrivateBotSettings(ctx, host, p, j, o, generation, record, snapshot, true)
}

func completePrivateBotSettings(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, snapshot privateBotSettingsSnapshot, applied bool) ([]privatechat.EffectRecord, error) {
	id := record.Effect.ID
	receipt, _ := json.Marshal(privateSettingsReceipt{Version: 1, EffectID: id, Record: snapshot.Record, Fingerprint: snapshot.Fingerprint, Revision: snapshot.Revision, Applied: applied})
	verifier := privateLocalVerifier{o, generation, record.Effect, receipt}
	if err := j.ConfirmLocalEffect(context.WithoutCancel(ctx), o, generation, id, receipt, verifier); err != nil {
		return nil, err
	}
	if err := host.refreshPrivateBotSettings(ctx, p, o, generation); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateSettingsReducer{host})
}

type privateSettingsReducer struct{ host *App }

func (r privateSettingsReducer) SettingsFanout(_ privatechat.Effect, raw json.RawMessage) (string, error) {
	var receipt privateSettingsReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return "", err
	}
	if !receipt.Applied {
		return "", nil
	}
	value, ok := r.host.privateSettingsCache.Load(receipt.Record.GatewayID)
	if !ok {
		return "", nil
	}
	entry := value.(privateBotSettingsCacheEntry)
	if entry.Snapshot.Fingerprint != receipt.Fingerprint || entry.Snapshot.Revision != r.host.privateSettingsRevision.Load() {
		return "", nil
	}
	return receipt.Fingerprint, nil
}

func (r privateSettingsReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var receipt privateSettingsReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	if event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandPrivateBotSettings || receipt.Version != 1 || receipt.EffectID != effect.ID {
		return nil, privatechat.ErrJournalConflict
	}
	var intent privateSettingsIntent
	if err = json.Unmarshal([]byte(event.DaemonCommand.Text), &intent); err != nil {
		return nil, err
	}
	if receipt.Record.GatewayID != intent.GatewayID || receipt.Fingerprint == "" {
		return nil, privatechat.ErrJournalConflict
	}
	wanted, ok := state.NormalizeBotCapabilitySettingsRecord(intent.Requested)
	if !ok {
		return nil, privatechat.ErrJournalConflict
	}
	wanted.UpdatedBy, wanted.UpdatedAt = intent.ActorID, time.Time{}
	actual := receipt.Record
	actual.UpdatedAt = time.Time{}
	wantedJSON, _ := json.Marshal(wanted)
	actualJSON, _ := json.Marshal(actual)
	if receipt.Applied && string(wantedJSON) != string(actualJSON) {
		return nil, privatechat.ErrJournalConflict
	}
	if !receipt.Applied {
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: intent.GatewayID, SurfaceSessionID: event.SurfaceSessionID, Notice: &control.Notice{Code: "private_settings_conflict", Title: "设置已变化", Text: "另一项设置刚刚更新，请查看当前配置后重试。"}}}, nil
	}
	cache, ok := r.host.privateSettingsCache.Load(intent.GatewayID)
	if !ok || cache.(privateBotSettingsCacheEntry).Snapshot.Fingerprint != receipt.Fingerprint || cache.(privateBotSettingsCacheEntry).Snapshot.Revision != r.host.privateSettingsRevision.Load() {
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: intent.GatewayID, SurfaceSessionID: event.SurfaceSessionID, Notice: &control.Notice{Code: "private_settings_superseded", Title: "设置已更新", Text: "本次设置保存后又发生了新的设置变更，请查看当前设置。"}}}, nil
	}
	var events []eventcontract.Event
	if intent.Transition != nil {
		if intent.Transition.SurfaceSessionID != event.SurfaceSessionID || intent.Transition.GatewayID != intent.GatewayID || intent.Transition.ActorUserID != intent.ActorID {
			return nil, privatechat.ErrJournalConflict
		}
		if !app.Service().PrivateRecoveryActionAllowed(*intent.Transition) {
			return nil, privatechat.ErrDeferred
		}
		events, err = app.Action(*intent.Transition)
		if err != nil {
			return nil, err
		}
	}
	if err = app.Service().ApplyPrivateBotSettingsToSurface(receipt.Record, event.SurfaceSessionID); err != nil {
		return nil, err
	}
	if intent.Transition != nil {
		follow, e := app.Service().ReconcilePrivateBotSettings(event.SurfaceSessionID)
		if e != nil {
			return nil, e
		}
		events = append(events, follow...)
	}
	if intent.CardMessageID != "" {
		for i := range events {
			if events[i].PageView != nil && events[i].InlineReplaceCurrentCard {
				events[i].PageView.MessageID = intent.CardMessageID
			}
		}
	}
	for _, rawEvent := range intent.Events {
		e, err := privatechat.DecodeEffectEvent(rawEvent)
		if err != nil {
			return nil, err
		}
		if intent.CardMessageID != "" && e.PageView != nil && e.InlineReplaceCurrentCard {
			e.PageView.MessageID = intent.CardMessageID
		}
		events = append(events, e)
	}
	return events, nil
}
