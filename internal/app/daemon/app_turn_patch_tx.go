package daemon

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	turnpatchruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/turnpatchruntime"
	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (a *App) newTurnPatchTransactionLocked(flow *turnpatchruntime.FlowRecord, kind turnpatchruntime.TransactionKind) *turnpatchruntime.Transaction {
	now := time.Now().UTC()
	a.turnPatchRuntime.NextTxSeq++
	tx := &turnpatchruntime.Transaction{
		ID:               fmt.Sprintf("turn-patch-tx-%d", a.turnPatchRuntime.NextTxSeq),
		FlowID:           strings.TrimSpace(flow.FlowID),
		RequestID:        strings.TrimSpace(flow.RequestID),
		Kind:             kind,
		InstanceID:       strings.TrimSpace(flow.InstanceID),
		InitiatorSurface: strings.TrimSpace(flow.SurfaceSessionID),
		InitiatorUserID:  strings.TrimSpace(flow.OwnerUserID),
		ThreadID:         strings.TrimSpace(flow.ThreadID),
		PatchID:          strings.TrimSpace(flow.PatchID),
		PausedSurfaceIDs: map[string]bool{},
		StartedAt:        now,
		UpdatedAt:        now,
	}
	switch kind {
	case turnpatchruntime.TransactionKindRollback:
		tx.Stage = turnpatchruntime.TransactionStageRollbackWrite
	default:
		tx.Stage = turnpatchruntime.TransactionStageApplyingWrite
	}
	a.pauseTurnPatchSurfacesLocked(tx)
	a.turnPatchRuntime.ActiveTx[tx.InstanceID] = tx
	return tx
}

func (a *App) pauseTurnPatchSurfacesLocked(tx *turnpatchruntime.Transaction) {
	if tx == nil {
		return
	}
	for _, surface := range a.service.Surfaces() {
		if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) != strings.TrimSpace(tx.InstanceID) {
			continue
		}
		a.service.PauseSurfaceDispatch(surface.SurfaceSessionID)
		tx.PausedSurfaceIDs[surface.SurfaceSessionID] = true
	}
}

func (a *App) finishTurnPatchTransactionLocked(tx *turnpatchruntime.Transaction) []eventcontract.Event {
	if tx == nil {
		return nil
	}
	active := a.turnPatchRuntime.ActiveTx[tx.InstanceID]
	if active == nil || strings.TrimSpace(active.ID) != strings.TrimSpace(tx.ID) {
		return nil
	}
	delete(a.turnPatchRuntime.ActiveTx, tx.InstanceID)
	surfaceIDs := make([]string, 0, len(tx.PausedSurfaceIDs))
	for surfaceID := range tx.PausedSurfaceIDs {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	events := make([]eventcontract.Event, 0, len(surfaceIDs))
	for _, surfaceID := range surfaceIDs {
		events = append(events, a.service.ResumeSurfaceDispatch(surfaceID, nil)...)
	}
	return events
}

func (a *App) activeTurnPatchTransactionByIDLocked(txID string) *turnpatchruntime.Transaction {
	txID = strings.TrimSpace(txID)
	if txID == "" {
		return nil
	}
	for _, tx := range a.turnPatchRuntime.ActiveTx {
		if tx == nil || strings.TrimSpace(tx.ID) != txID {
			continue
		}
		return tx
	}
	return nil
}

func (a *App) runTurnPatchApplyTransaction(txID string) {
	a.mu.Lock()
	tx := a.activeTurnPatchTransactionByIDLocked(txID)
	flow := a.findTurnPatchFlowByFlowIDLocked(txFlowID(tx))
	storage := a.turnPatchRuntime.Storage
	req := turnPatchApplyRequest(flow)
	a.mu.Unlock()
	if tx == nil || flow == nil || storage == nil {
		return
	}

	result, err := storage.ApplyLatestTurnPatch(req)
	if err != nil {
		a.mu.Lock()
		events := a.finishTurnPatchFailureLocked(tx, "当前会话修补失败", turnPatchApplyFailureLines(err)...)
		a.handleUIEventsLocked(context.Background(), events)
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	tx = a.activeTurnPatchTransactionByIDLocked(txID)
	flow = a.findTurnPatchFlowByFlowIDLocked(txFlowID(tx))
	if tx == nil || flow == nil {
		a.mu.Unlock()
		return
	}
	flow.PatchID = strings.TrimSpace(result.PatchID)
	flow.BackupPath = strings.TrimSpace(result.BackupPath)
	flow.ReplacedCount = result.ReplacedMessageCount
	flow.RemovedReasoning = result.RemovedReasoningLine
	tx.PatchID = strings.TrimSpace(result.PatchID)
	tx.Stage = turnpatchruntime.TransactionStageApplyingRestart
	tx.UpdatedAt = time.Now().UTC()
	a.mu.Unlock()

	restartCtx, cancel := context.WithTimeout(context.Background(), childRestartOutcomeTimeout)
	defer cancel()
	if err := a.restartRelayChildCodexAndWait(restartCtx, tx.InstanceID); err != nil {
		a.mu.Lock()
		tx = a.activeTurnPatchTransactionByIDLocked(txID)
		if tx != nil {
			tx.UpdatedAt = time.Now().UTC()
			a.startTurnPatchApplyRecoveryLocked(tx, err.Error())
		}
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	tx = a.activeTurnPatchTransactionByIDLocked(txID)
	if tx != nil {
		events := a.finishTurnPatchApplySuccessLocked(tx)
		a.handleUIEventsLocked(context.Background(), events)
	}
	a.mu.Unlock()
}

func (a *App) runTurnPatchRollbackTransaction(txID string) {
	a.mu.Lock()
	tx := a.activeTurnPatchTransactionByIDLocked(txID)
	flow := a.findTurnPatchFlowByFlowIDLocked(txFlowID(tx))
	storage := a.turnPatchRuntime.Storage
	req := turnPatchRollbackRequest(tx, flow)
	a.mu.Unlock()
	if tx == nil || flow == nil || storage == nil {
		return
	}

	if _, err := storage.RollbackLatestTurnPatch(req); err != nil {
		a.mu.Lock()
		events := a.finishTurnPatchFailureLocked(tx, "当前会话回滚失败", turnPatchRollbackFailureLines(err)...)
		a.handleUIEventsLocked(context.Background(), events)
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	tx = a.activeTurnPatchTransactionByIDLocked(txID)
	if tx != nil {
		tx.Stage = turnpatchruntime.TransactionStageRollbackRestart
		tx.UpdatedAt = time.Now().UTC()
	}
	a.mu.Unlock()

	restartCtx, cancel := context.WithTimeout(context.Background(), childRestartOutcomeTimeout)
	defer cancel()
	if err := a.restartRelayChildCodexAndWait(restartCtx, tx.InstanceID); err != nil {
		a.mu.Lock()
		tx = a.activeTurnPatchTransactionByIDLocked(txID)
		if tx != nil {
			tx.UpdatedAt = time.Now().UTC()
			events := a.finishTurnPatchFailureLocked(
				tx,
				"当前会话回滚失败",
				append(turnPatchRollbackFailureLines(nil), "原始内容已写回磁盘，但 child restart 失败："+err.Error())...,
			)
			a.handleUIEventsLocked(context.Background(), events)
		}
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	tx = a.activeTurnPatchTransactionByIDLocked(txID)
	if tx != nil {
		events := a.finishTurnPatchRollbackSuccessLocked(tx)
		a.handleUIEventsLocked(context.Background(), events)
	}
	a.mu.Unlock()
}

func (a *App) runTurnPatchApplyRecoveryTransaction(txID, reason string) {
	a.mu.Lock()
	tx := a.activeTurnPatchTransactionByIDLocked(txID)
	flow := a.findTurnPatchFlowByFlowIDLocked(txFlowID(tx))
	storage := a.turnPatchRuntime.Storage
	req := turnPatchRollbackRequest(tx, flow)
	a.mu.Unlock()
	if tx == nil || flow == nil || storage == nil {
		return
	}

	if _, err := storage.RollbackLatestTurnPatch(req); err != nil {
		a.mu.Lock()
		events := a.finishTurnPatchFailureLocked(
			tx,
			"当前会话修补失败",
			append([]string{"child restart 失败后自动回滚也没有完成。", reason}, turnPatchRollbackFailureLines(err)...)...,
		)
		a.handleUIEventsLocked(context.Background(), events)
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	tx = a.activeTurnPatchTransactionByIDLocked(txID)
	if tx != nil {
		tx.Stage = turnpatchruntime.TransactionStageApplyRecoveryRestart
		tx.UpdatedAt = time.Now().UTC()
	}
	a.mu.Unlock()

	restartCtx, cancel := context.WithTimeout(context.Background(), childRestartOutcomeTimeout)
	defer cancel()
	if err := a.restartRelayChildCodexAndWait(restartCtx, tx.InstanceID); err != nil {
		a.mu.Lock()
		tx = a.activeTurnPatchTransactionByIDLocked(txID)
		if tx != nil {
			tx.UpdatedAt = time.Now().UTC()
			events := a.finishTurnPatchFailureLocked(
				tx,
				"当前会话修补失败",
				"修补没有生效，磁盘内容已恢复到修补前状态。",
				"但恢复运行态时再次发送 child restart 失败："+err.Error(),
			)
			a.handleUIEventsLocked(context.Background(), events)
		}
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	tx = a.activeTurnPatchTransactionByIDLocked(txID)
	if tx != nil {
		events := a.finishTurnPatchFailureLocked(tx, "当前会话修补失败", "修补没有生效，已自动恢复到修补前状态。")
		a.handleUIEventsLocked(context.Background(), events)
	}
	a.mu.Unlock()
}

func (a *App) startTurnPatchApplyRecoveryLocked(tx *turnpatchruntime.Transaction, reason string) {
	if tx == nil {
		return
	}
	tx.Stage = turnpatchruntime.TransactionStageApplyRecoveryRollback
	tx.UpdatedAt = time.Now().UTC()
	go a.runTurnPatchApplyRecoveryTransaction(tx.ID, strings.TrimSpace(firstNonEmpty(reason, "child restart 失败。")))
}

func (a *App) finishTurnPatchApplySuccessLocked(tx *turnpatchruntime.Transaction) []eventcontract.Event {
	flow := a.findTurnPatchFlowByFlowIDLocked(txFlowID(tx))
	if flow == nil {
		return a.finishTurnPatchTransactionLocked(tx)
	}
	a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageApplied)
	flow.AppliedAt = time.Now().UTC()
	events := []eventcontract.Event{
		turnPatchPageEvent(flow.SurfaceSessionID, turnPatchAppliedPageView(flow), false),
	}
	return append(events, a.finishTurnPatchTransactionLocked(tx)...)
}

func (a *App) finishTurnPatchRollbackSuccessLocked(tx *turnpatchruntime.Transaction) []eventcontract.Event {
	flow := a.findTurnPatchFlowByFlowIDLocked(txFlowID(tx))
	if flow == nil {
		return a.finishTurnPatchTransactionLocked(tx)
	}
	a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageRolledBack)
	flow.RolledBackAt = time.Now().UTC()
	events := []eventcontract.Event{
		turnPatchPageEvent(flow.SurfaceSessionID, turnPatchRolledBackPageView(flow), false),
	}
	return append(events, a.finishTurnPatchTransactionLocked(tx)...)
}

func (a *App) finishTurnPatchFailureLocked(tx *turnpatchruntime.Transaction, title string, lines ...string) []eventcontract.Event {
	flow := a.findTurnPatchFlowByFlowIDLocked(txFlowID(tx))
	events := []eventcontract.Event{}
	if flow != nil {
		a.refreshTurnPatchFlowLocked(flow, turnpatchruntime.FlowStageFailed)
		events = append(events, turnPatchPageEvent(flow.SurfaceSessionID, turnPatchFailedPageView(flow, title, lines...), false))
	}
	return append(events, a.finishTurnPatchTransactionLocked(tx)...)
}

func txFlowID(tx *turnpatchruntime.Transaction) string {
	if tx == nil {
		return ""
	}
	return strings.TrimSpace(tx.FlowID)
}

func turnPatchApplyRequest(flow *turnpatchruntime.FlowRecord) codexstate.ApplyLatestTurnPatchRequest {
	req := codexstate.ApplyLatestTurnPatchRequest{}
	if flow == nil {
		return req
	}
	replacements := make([]codexstate.TurnPatchReplacement, 0, len(flow.Candidates))
	for _, candidate := range flow.Candidates {
		value := strings.TrimSpace(flow.Answers[candidate.QuestionID])
		if value == "" {
			value = strings.TrimSpace(candidate.DefaultText)
		}
		replacements = append(replacements, codexstate.TurnPatchReplacement{
			MessageKey: strings.TrimSpace(candidate.MessageKey),
			NewText:    value,
		})
	}
	return codexstate.ApplyLatestTurnPatchRequest{
		ThreadID:              strings.TrimSpace(flow.ThreadID),
		ExpectedTurnID:        strings.TrimSpace(flow.TurnID),
		ExpectedRolloutDigest: strings.TrimSpace(flow.RolloutDigest),
		Replacements:          replacements,
		ActorUserID:           strings.TrimSpace(flow.OwnerUserID),
		SurfaceSessionID:      strings.TrimSpace(flow.SurfaceSessionID),
	}
}

func turnPatchRollbackRequest(tx *turnpatchruntime.Transaction, flow *turnpatchruntime.FlowRecord) codexstate.RollbackLatestTurnPatchRequest {
	req := codexstate.RollbackLatestTurnPatchRequest{}
	if tx == nil {
		return req
	}
	req.ThreadID = strings.TrimSpace(tx.ThreadID)
	req.PatchID = strings.TrimSpace(tx.PatchID)
	req.ActorUserID = strings.TrimSpace(tx.InitiatorUserID)
	if flow != nil {
		req.ThreadID = strings.TrimSpace(firstNonEmpty(req.ThreadID, flow.ThreadID))
		req.PatchID = strings.TrimSpace(firstNonEmpty(req.PatchID, flow.PatchID))
		req.ActorUserID = strings.TrimSpace(firstNonEmpty(req.ActorUserID, flow.OwnerUserID))
	}
	return req
}
