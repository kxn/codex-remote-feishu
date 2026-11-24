package daemon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/nativeinspection"
)

type privateStopReceipt struct {
	Version              int
	EffectID, InstanceID string
}
type privateStopReducer struct{ host *App }

func (r privateStopReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var receipt privateStopReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != effect.ID || event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandKillHeadless || receipt.InstanceID != event.DaemonCommand.InstanceID {
		return nil, privatechat.ErrJournalConflict
	}
	app.Service().RemoveInstance(receipt.InstanceID)
	return nil, nil
}
func privateStopCommandID(effect, kind, session, turn string) string {
	sum := sha256.Sum256([]byte(effect + "\x00" + kind + "\x00" + session + "\x00" + turn))
	return "private-stop-" + hex.EncodeToString(sum[:20])
}
func (r *privateWorkRunner) runHeadlessStop(ctx context.Context, work privatechat.PendingEffectWork) error {
	effect, err := r.journal.LoadEffect(ctx, work.Owner, work.Generation, work.Effect.Effect.ID)
	if err != nil {
		return err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Effect.Body)
	if err != nil {
		return err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandKillHeadless || cmd.InstanceID == "" || cmd.SurfaceSessionID != work.Owner.SurfaceID || event.SurfaceSessionID != work.Owner.SurfaceID {
		return privatechat.ErrJournalConflict
	}
	if effect.State == "confirmed" {
		_, err = r.processor.ApplyLocalHostReceipt(ctx, work.Owner, work.Generation, effect.Effect.ID, privateStopReducer{r.host})
		return err
	}
	rec, err := r.native.store.lookup(ctx, cmd.InstanceID)
	if errors.Is(err, sql.ErrNoRows) {
		if err = r.journal.ProveCancelledLaunchWithoutCarrier(ctx, work.Owner, work.Generation, cmd.InstanceID); err != nil {
			return err
		}
		// A superseded launch is already durably applied and cannot be scheduled
		// again. Verify absence once more before recording the local stop result.
		if _, err = r.native.store.lookup(ctx, cmd.InstanceID); !errors.Is(err, sql.ErrNoRows) {
			if err == nil {
				return privatechat.ErrDeferred
			}
			return err
		}
		return r.confirmHeadlessStop(ctx, work, effect, cmd.InstanceID)
	}
	if err != nil {
		return err
	}
	if rec.Surface != work.Owner || rec.SurfaceGeneration != work.Generation {
		return privatechat.ErrJournalConflict
	}
	if err = r.native.advanceCarrierStop(ctx, rec, effect.Effect.ID); err != nil {
		return err
	}
	return r.confirmHeadlessStop(ctx, work, effect, cmd.InstanceID)
}

// advanceCarrierStop persists and advances the same stop evidence chain for user and daemon shutdown requests.
func (h *privateNativeHost) advanceCarrierStop(ctx context.Context, rec privateCarrierRecord, stopID string) error {
	instance := rec.Snapshot.Owner.InstanceID
	var err error
	if rec.State == "running" {
		if err = h.beginDrainForStop(ctx, instance); err != nil {
			return err
		}
		rec.State = "draining"
	}
	if rec.State != "draining" && rec.State != "sealed" && rec.State != "retired" {
		return privatechat.ErrDeferred
	}
	rec, err = h.store.stopIntent(ctx, instance, stopID)
	if err != nil {
		return err
	}
	if rec.State == "draining" {
		lease, e := h.lease(ctx, rec, nil)
		if e != nil {
			return e
		}
		turns, e := lease.ActiveAcceptedTurns(ctx)
		if e != nil {
			return e
		}
		h.mu.Lock()
		connection := h.connections[instance]
		h.mu.Unlock()
		meta := relayws.ConnectionMeta{ConnectionID: connection, PrivateAuthenticated: true}
		for _, turn := range turns {
			id := privateStopCommandID(stopID, "interrupt", turn.SessionID, turn.TurnID)
			_, e = lease.HostCommandState(ctx, id)
			if errors.Is(e, sql.ErrNoRows) {
				if connection == 0 {
					return privatechat.ErrDeferred
				}
				if e = h.sendStopCommand(ctx, meta, instance, id, &turn); e != nil {
					return e
				}
			} else if e != nil {
				return e
			}
		}
		if len(turns) != 0 {
			return privatechat.ErrDeferred
		}
		rec, e = h.store.inspectionIntent(ctx, instance, stopID, false)
		if e != nil {
			return e
		}
		_, e = lease.HostCommandState(ctx, rec.StopInspectionID)
		if errors.Is(e, sql.ErrNoRows) {
			if connection == 0 {
				return privatechat.ErrDeferred
			}
			_, e = h.sendInspection(ctx, meta, instance, rec.StopInspectionID)
			if e != nil {
				return e
			}
			return privatechat.ErrDeferred
		}
		if e != nil {
			return e
		}
		ref, e := lease.FindNativeEvent(ctx, rec.StopInspectionID, agentproto.EventRuntimeInspectionCompleted)
		if errors.Is(e, sql.ErrNoRows) {
			return privatechat.ErrDeferred
		}
		if e != nil {
			return e
		}
		inspection, e := lease.CommittedNativeInspection(ctx, ref)
		if e != nil {
			return e
		}
		e = nativeinspection.VerifyPrivateOwnerStoppedWithTerminalProof(rec.Snapshot.Owner, inspection, func(event agentproto.Event) bool {
			return h.journal.ProveCommittedNativeTerminal(ctx, rec.Snapshot.Owner, rec.Surface, rec.SurfaceGeneration, event) == nil
		})
		if e != nil {
			_, _ = h.store.inspectionIntent(ctx, instance, stopID, true)
			return privatechat.ErrDeferred
		}
		exitID := privateStopCommandID(stopID, "exit", "", "")
		_, e = lease.HostCommandState(ctx, exitID)
		if errors.Is(e, sql.ErrNoRows) {
			if connection == 0 {
				return privatechat.ErrDeferred
			}
			if e = h.sendStopCommand(ctx, meta, instance, exitID, nil); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		if e = h.sealDrain(ctx, instance, ref); e != nil {
			return e
		}
	}
	err = h.retire(ctx, instance)
	if err != nil {
		return fmt.Errorf("retire private carrier: %w", err)
	}
	return nil
}

func (r *privateWorkRunner) confirmHeadlessStop(ctx context.Context, work privatechat.PendingEffectWork, effect privatechat.EffectRecord, instance string) error {
	if effect.State == "prepared" {
		if err := r.journal.MarkEffectSending(ctx, work.Owner, work.Generation, effect.Effect.ID); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(privateStopReceipt{Version: 1, EffectID: effect.Effect.ID, InstanceID: instance})
	if err := r.journal.ConfirmLocalEffect(ctx, work.Owner, work.Generation, effect.Effect.ID, raw, privateLocalVerifier{owner: work.Owner, generation: work.Generation, effect: effect.Effect, receipt: raw}); err != nil {
		return err
	}
	_, err := r.processor.ApplyLocalHostReceipt(ctx, work.Owner, work.Generation, effect.Effect.ID, privateStopReducer{r.host})
	return err
}

func (h *privateNativeHost) projectStoppedCarrierEvents(ctx context.Context, rec privateCarrierRecord) error {
	lease, err := h.lease(ctx, rec, nil)
	if err != nil {
		return err
	}
	for {
		pending, e := lease.PendingBusinessNativeEvents(ctx, 0, 128)
		if e != nil {
			return e
		}
		if len(pending) > 0 && h.processor == nil {
			return fmt.Errorf("private stop projection processor unavailable")
		}
		for _, item := range pending {
			if _, e = h.processor.ApplyNativeForStop(ctx, lease, item.Ref, privatechat.CoreNativeReducer{}); e != nil {
				return fmt.Errorf("project stopped event kind=%s sequence=%d: %w", item.Event.Kind, item.Ref.Sequence, e)
			}
		}
		if len(pending) < 128 {
			return nil
		}
	}
}
