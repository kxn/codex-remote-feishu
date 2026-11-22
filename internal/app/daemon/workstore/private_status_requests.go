package workstore

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) privateStatusAnswerableRequests(wc work.WorkContext) (int, error) {
	worker, err := t.CurrentContextWorker(wc.ID)
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if worker.Controller != work.ControllerAutomatic || worker.CarrierIncarnation == "" || worker.NativeSessionID == "" {
		return 0, nil
	}
	carrier, err := t.GetCarrier(worker.CarrierIncarnation)
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !carrier.Matches(worker) {
		return 0, nil
	}
	bot, err := t.GetRoleBotBinding(worker.RoleID)
	if err != nil {
		return 0, err
	}
	attempt, err := t.GetOccupiedAttemptForWorker(worker.ID)
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if attempt.State != work.AttemptRunning {
		return 0, nil
	}
	occupied, err := t.HasCarrierOccupancy(carrier, attempt.ID)
	if err != nil || !occupied {
		return 0, err
	}
	gates, err := t.ListPauseGates(wc.ScopeID)
	if err != nil {
		return 0, err
	}
	allowed, err := work.GatesAllowRunForPurpose(wc.ScopeID, attempt.TaskID, wc.ID, wc.Purpose, gates)
	if err != nil || !allowed {
		return 0, err
	}
	requests, err := listRecords[NativeRequest](t, "native_ui_requests", " WHERE scope_id=? AND worker_id=? AND state='open'", wc.ScopeID, worker.ID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, request := range requests {
		if request.State != "open" || request.CarrierIncarnation != carrier.Incarnation || request.NativeSessionID != worker.NativeSessionID || request.Binding.AttemptID != attempt.ID || request.Binding.RunRef != attempt.RunRef.RunID || request.Binding.BotGeneration != bot.IdentityGeneration || !carrier.Owner.SameOwner(request.Binding) {
			continue
		}
		bound, err := t.ValidateNativeBinding(request.Binding)
		if err != nil {
			return 0, err
		}
		if bound.ID != attempt.ID {
			continue
		}
		turn, err := t.GetNativeTurn(carrier.Incarnation, request.NativeSessionID, request.NativeTurnID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if turn.Binding != request.Binding || turn.TerminalStatus != "" {
			continue
		}
		count++
	}
	return count, nil
}
