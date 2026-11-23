package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// sendStopCommand uses only durable host stop intent and the normal outbound
// gate. A registered command is reconciled from the native journal, never resent.
func (h *privateNativeHost) sendStopCommand(ctx context.Context, meta relayws.ConnectionMeta, instance, id string, turn *privatechat.AcceptedNativeTurn) error {
	if !h.validConnection(instance, meta) {
		return fmt.Errorf("authenticated connection required")
	}
	h.drainMu.Lock()
	defer h.drainMu.Unlock()
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return err
	}
	if rec.State != "draining" || rec.StopEffectID == "" || id == "" {
		return privatechat.ErrJournalConflict
	}
	owner := rec.Snapshot.Owner
	binding := agentproto.NativeTurnBinding{Version: agentproto.PrivateNativeProtocolVersion, Owner: owner}
	kind, lane, mode := agentproto.CommandProcessExit, "inspect", "start"
	session := privatechat.NativeSession{Backend: string(owner.Backend), Namespace: owner.HistoryNamespace, CarrierID: owner.CarrierIncarnation, CarrierGeneration: owner.InstanceGeneration}
	command := agentproto.Command{CommandID: id}
	if turn != nil {
		binding = turn.Binding
		kind, lane, mode = agentproto.CommandTurnInterrupt, "control", "resume"
		session.SessionID = turn.SessionID
		command.Target = agentproto.Target{ThreadID: turn.SessionID, TurnID: turn.TurnID}
	}
	command.Kind, command.NativeBinding = kind, &binding
	dispatch := privatechat.Dispatch{ID: id, CommandID: id, Lane: lane, Mode: mode, Owner: rec.Surface, Generation: rec.SurfaceGeneration, Session: session}
	prepared := false
	err = h.outbound.SendLeased(ctx, privateRegistration(owner), func(token *executionowner.Lease) error {
		lease, e := h.lease(ctx, rec, token)
		if e != nil {
			return e
		}
		if turn == nil {
			e = lease.PrepareHostExit(ctx, dispatch, command)
		} else {
			e = lease.PrepareHostStop(ctx, dispatch, command)
		}
		if e != nil {
			return e
		}
		prepared = true
		return lease.MarkSending(ctx, id)
	}, func() error { return h.relay.SendCommandToConnection(instance, meta.ConnectionID, command) })
	if err != nil && prepared {
		if lease, e := h.lease(ctx, rec, nil); e == nil {
			_ = lease.MarkDispatchUnknown(ctx, id, err.Error())
		}
	}
	return err
}
