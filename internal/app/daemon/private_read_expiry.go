package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// This receipt records expiry of local observation, not native execution or a
// claim that the query was not sent. The dispatch and native journal remain.
type privateReadExpiryReceipt struct {
	Version    int    `json:"version"`
	Kind       string `json:"kind"`
	EffectID   string `json:"effectId"`
	DispatchID string `json:"dispatchId"`
	CommandID  string `json:"commandId"`
	InstanceID string `json:"instanceId"`
	SessionID  string `json:"sessionId"`
	ExpiresNS  int64  `json:"expiresNs"`
}

type privateReadExpiryReducer struct{}

func (privateReadExpiryReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	var receipt privateReadExpiryReceipt
	event, _, target, err := privatechat.DecodeStoredEffectTarget(effect.Body)
	if err != nil || json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.Kind != "native_read_observation_expired" || receipt.EffectID != effect.ID || receipt.DispatchID != effect.ID+":native" || receipt.ExpiresNS <= 0 || event.Command == nil || privateSessionCommandScope(event.Command.Kind) != privateSessionRead || event.Command.CommandID != receipt.CommandID || event.Command.Target.ThreadID != receipt.SessionID || target == nil || *target != receipt.InstanceID {
		return nil, privatechat.ErrJournalConflict
	}
	events, _ := app.Service().HandlePrivateReadObservationTimeout(event.SurfaceSessionID, receipt.InstanceID, receipt.CommandID, errors.New("等待会话查询结果超时，请重试。"))
	return events, nil
}

func (r *privateWorkRunner) expireNativeReadObservation(ctx context.Context, work privatechat.PendingEffectWork, rec privateCarrierRecord, command agentproto.Command) error {
	if privateSessionCommandScope(command.Kind) != privateSessionRead || (command.Target.ThreadID == "" && command.Kind != agentproto.CommandMCPOAuthLogin) {
		return nil
	}
	id := work.Effect.Effect.ID
	dispatch := id + ":native"
	reg := privateRegistration(rec.Snapshot.Owner)
	deadline, err := r.journal.NativeReadDeadline(ctx, work.Owner, work.Generation, dispatch)
	if err != nil || deadline.IsZero() || deadline.After(time.Now()) {
		return err
	}
	var receipt json.RawMessage
	record := func(expires int64) error {
		state, e := r.journal.DispatchState(ctx, dispatch)
		if e != nil {
			return e
		}
		if state != "sending" && state != "sent" && state != "unknown" {
			return nil
		}
		if work.Effect.State == "prepared" {
			if e = r.journal.MarkEffectSending(ctx, work.Owner, work.Generation, id); e != nil {
				return e
			}
		}
		receipt, e = json.Marshal(privateReadExpiryReceipt{Version: 1, Kind: "native_read_observation_expired", EffectID: id, DispatchID: dispatch, CommandID: command.CommandID, InstanceID: rec.Snapshot.Owner.InstanceID, SessionID: command.Target.ThreadID, ExpiresNS: expires})
		if e != nil {
			return e
		}
		return r.journal.ConfirmLocalEffect(ctx, work.Owner, work.Generation, id, receipt, privateLocalVerifier{owner: work.Owner, generation: work.Generation, effect: work.Effect.Effect, receipt: receipt})
	}
	if rec.State == "retired" {
		err = r.native.WithRetiredRuntime(ctx, work.Owner, work.Generation, rec.Snapshot.Owner, func() error {
			return r.native.registry.WithRetiredReadExpiration(ctx, reg, privateCarrierStopEvidence(rec), command.Target.ThreadID, dispatch, deadline.UnixNano(), record)
		})
	} else {
		err = r.native.registry.WithScopedLease(ctx, reg, func(token *executionowner.Lease) error {
			if command.Target.ThreadID == "" {
				return record(deadline.UnixNano())
			}
			expires, e := token.SessionReadDeadline(reg, command.Target.ThreadID, dispatch)
			if e != nil {
				return e
			}
			if expires != 0 && expires != deadline.UnixNano() {
				return executionowner.ErrConflict
			}
			if e = record(deadline.UnixNano()); e != nil {
				return e
			}
			if len(receipt) > 0 {
				return token.FinishSessionRead(reg, command.Target.ThreadID, dispatch)
			}
			return nil
		})
	}
	if err != nil || len(receipt) == 0 {
		return err
	}
	_, err = r.processor.ApplyLocalHostReceipt(ctx, work.Owner, work.Generation, id, privateReadExpiryReducer{})
	return err
}
