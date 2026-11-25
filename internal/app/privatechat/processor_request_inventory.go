package privatechat

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func requestInventoryEvent(l *NativeLease) eventcontract.Event {
	return eventcontract.Event{Kind: eventcontract.KindAgentCommand, GatewayID: l.surface.GatewayID, SurfaceSessionID: l.surface.SurfaceID, Command: &agentproto.Command{Kind: agentproto.CommandRuntimeInspect, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: agentproto.RuntimeInspectionProtocolVersion, ExpectedInstanceGeneration: l.native.InstanceGeneration, ExpectedProcessGeneration: l.native.ProcessGeneration, IncludePendingRequests: true}}}
}

func (p *Processor) requestInventoryRetryDue(ctx context.Context, l *NativeLease, now time.Time) (bool, error) {
	due, ok := p.requestInventoryRetry[l.native.CarrierIncarnation]
	if !ok || now.Before(due) {
		return false, nil
	}
	held, ok := p.app.Service().PrivateRecoveryCheckpoint(l.surface.SurfaceID)
	if !ok {
		delete(p.requestInventoryRetry, l.native.CarrierIncarnation)
		return false, nil
	}
	found := false
	for _, h := range held.Holds {
		if h.Kind == "requests" && h.InstanceID == l.native.InstanceID {
			found = true
			break
		}
	}
	if !found {
		delete(p.requestInventoryRetry, l.native.CarrierIncarnation)
		return false, nil
	}
	var pending int
	err := p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND json_extract(e.body,'$.event.Command.kind')=? AND (e.state<>'confirmed' OR EXISTS (SELECT 1 FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=json_extract(e.body,'$.event.Command.commandId') AND d.carrier=? AND d.state IN ('prepared','sending','unknown','sent')))`, l.surface.TenantID, l.surface.AppID, l.surface.GatewayID, l.surface.SurfaceID, l.surface.ChatID, l.surface.UserID, l.generation, string(agentproto.CommandRequestRespond), l.native.CarrierIncarnation).Scan(&pending)
	return pending == 0, err
}

// restoreRequestInventoryLocked is called under the processor and exact live
// native lease. An inspection lacking the new capability is never proof that
// a request disappeared; its held checkpoint is simply left untouched.
func (p *Processor) restoreRequestInventoryLocked(ctx context.Context, l *NativeLease, ref NativeEventRef, event agentproto.Event) (bool, error) {
	if event.Kind != agentproto.EventRuntimeInspectionCompleted || event.RuntimeInspection == nil {
		return false, nil
	}
	inv := event.RuntimeInspection.PendingRequests
	var raw []byte
	var dispatchState string
	var eventSequence int64
	if err := p.journal.db.QueryRowContext(ctx, `SELECT n.payload,d.state,d.event_sequence FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=? AND d.carrier=? AND d.tenant=? AND d.app_id=? AND d.gateway=? AND d.surface=? AND d.chat=? AND d.user_id=? AND d.generation=?`, event.CommandID, l.native.CarrierIncarnation, l.surface.TenantID, l.surface.AppID, l.surface.GatewayID, l.surface.SurfaceID, l.surface.ChatID, l.surface.UserID, l.generation).Scan(&raw, &dispatchState, &eventSequence); err != nil {
		return false, err
	}
	var command agentproto.Command
	if json.Unmarshal(raw, &command) != nil || command.Kind != agentproto.CommandRuntimeInspect || command.RuntimeInspection == nil {
		return false, nil
	}
	if !command.RuntimeInspection.IncludePendingRequests {
		return false, nil
	}
	if command.NativeBinding == nil || command.NativeBinding.Owner != l.native || command.NativeBinding.ExecutionID != "" || dispatchState != "accepted" || eventSequence != int64(ref.Sequence) {
		return false, ErrJournalConflict
	}
	if inv == nil {
		if p.requestInventoryUnsupported == nil {
			p.requestInventoryUnsupported = map[Owner]bool{}
		}
		p.requestInventoryUnsupported[l.surface] = true
		delete(p.requestInventoryRetry, l.native.CarrierIncarnation)
		return false, nil
	}
	if inv.Version != 1 || inv.CarrierIncarnation != l.native.CarrierIncarnation || inv.InstanceGeneration != l.native.InstanceGeneration || inv.ProcessGeneration != l.native.ProcessGeneration || inv.EventSequence >= ref.Sequence || event.RuntimeInspection.Problem != nil {
		return false, ErrJournalConflict
	}
	if !inv.Complete {
		if p.requestInventoryRetry == nil {
			p.requestInventoryRetry = map[string]time.Time{}
		}
		p.requestInventoryRetry[l.native.CarrierIncarnation] = time.Now().Add(5 * time.Second)
		return false, nil
	}
	delete(p.requestInventoryRetry, l.native.CarrierIncarnation)
	delete(p.requestInventoryUnsupported, l.surface)
	var count, applied int64
	if err := p.journal.db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(applied),0) FROM native_events WHERE incarnation=? AND sequence<=?`, l.native.CarrierIncarnation, inv.EventSequence).Scan(&count, &applied); err != nil {
		return false, err
	}
	if count != int64(inv.EventSequence) || applied != count {
		return false, ErrJournalState
	}
	held, ok := p.app.Service().PrivateRecoveryCheckpoint(l.surface.SurfaceID)
	if !ok {
		return false, nil
	}
	known := map[string]*state.RequestPromptRecord{}
	for _, h := range held.Holds {
		if h.Kind != "requests" || h.InstanceID != l.native.InstanceID {
			continue
		}
		var data struct {
			Requests map[string]*state.RequestPromptRecord
		}
		if json.Unmarshal(h.Frozen, &data) != nil {
			return false, ErrJournalConflict
		}
		for id, req := range data.Requests {
			if _, exists := known[id]; exists {
				return false, ErrJournalConflict
			}
			known[id] = req
		}
	}
	seen := map[string]bool{}
	var restore []agentproto.RuntimePendingRequest
	for _, item := range inv.Requests {
		if item.RequestID == "" || item.ThreadID == "" || item.TurnID == "" || item.StartedSequence == 0 || item.StartedSequence > inv.EventSequence || item.StartedHash == "" || seen[item.RequestID] {
			return false, ErrJournalConflict
		}
		seen[item.RequestID] = true
		req := known[item.RequestID]
		if req == nil {
			continue
		}
		if req.InstanceID != l.native.InstanceID || req.ThreadID != item.ThreadID || req.TurnID != item.TurnID {
			return false, ErrJournalConflict
		}
		var source []byte
		var hash string
		var sourceApplied int
		if err := p.journal.db.QueryRowContext(ctx, `SELECT payload,payload_hash,applied FROM native_events WHERE incarnation=? AND sequence=?`, l.native.CarrierIncarnation, item.StartedSequence).Scan(&source, &hash, &sourceApplied); err != nil {
			return false, err
		}
		var started agentproto.Event
		if json.Unmarshal(source, &started) != nil || sourceApplied != 1 || hash != item.StartedHash || started.Kind != agentproto.EventRequestStarted || started.RequestID != item.RequestID || started.ThreadID != item.ThreadID || started.TurnID != item.TurnID || started.NativeBinding != nil && started.NativeBinding.Owner != l.native {
			return false, ErrJournalConflict
		}
		digest, err := agentproto.RuntimeEventPayloadHash(started)
		if err != nil || digest != hash {
			return false, ErrJournalConflict
		}
		var resolved int
		if err = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM native_events WHERE incarnation=? AND sequence>? AND sequence<? AND json_extract(payload,'$.requestId')=? AND (json_extract(payload,'$.kind') IN (?,?) OR json_extract(payload,'$.action')='request.response.written')`, l.native.CarrierIncarnation, item.StartedSequence, ref.Sequence, item.RequestID, string(agentproto.EventRequestResolved), string(agentproto.EventRequestStarted)).Scan(&resolved); err != nil {
			return false, err
		}
		if resolved != 0 {
			continue
		}
		// A response intent can be committed after the wrapper's snapshot but
		// before its written event. It is not safe to make that request clickable
		// again while the local effect or its native dispatch is unresolved.
		var responding int
		if err = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND json_extract(e.body,'$.event.Command.kind')=? AND json_extract(e.body,'$.event.Command.request.requestId')=? AND (e.state<>'confirmed' OR EXISTS (SELECT 1 FROM native_commands n JOIN dispatch d ON d.id=n.dispatch_id WHERE n.command_id=json_extract(e.body,'$.event.Command.commandId') AND d.carrier=? AND d.state<>'failed'))`, l.surface.TenantID, l.surface.AppID, l.surface.GatewayID, l.surface.SurfaceID, l.surface.ChatID, l.surface.UserID, l.generation, string(agentproto.CommandRequestRespond), item.RequestID, l.native.CarrierIncarnation).Scan(&responding); err != nil {
			return false, err
		}
		if responding != 0 {
			continue
		}
		restore = append(restore, item)
	}
	for _, item := range restore {
		if err := p.app.Service().RestorePrivateRequestProjection(l.surface.SurfaceID, l.native.InstanceID, item.RequestID, "", item.ThreadID, item.TurnID); err != nil {
			p.poisoned = true
			return false, err
		}
		if err := p.app.Service().RefreshPrivateRequestVisibility(l.surface.SurfaceID, item.RequestID); err != nil {
			p.poisoned = true
			return false, err
		}
	}
	return len(restore) != 0, nil
}
