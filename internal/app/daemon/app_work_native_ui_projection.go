package daemon

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

func (a *App) presentTeamPrivateNativeResult(tx *workstore.Tx, op work.Operation, panel workui.Panel, now time.Time, verifySession bool) error {
	pendingID := ""
	incarnation := ""
	for _, step := range op.Steps {
		if step.ID == "ui_pending" {
			pendingID = step.Receipt
		}
		if step.ID == "carrier" {
			incarnation = step.Receipt
		}
	}
	view, err := tx.GetUIView(pendingID)
	if err != nil {
		return err
	}
	nav, err := tx.GetPrivateNavigation(op.ScopeID)
	if err != nil {
		return err
	}
	binding, err := tx.GetRoleBotBinding(view.Binding.RoleID)
	if err != nil {
		return err
	}
	currentSession := true
	if verifySession {
		worker, err := tx.GetWorker(op.TargetID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(op.ScopeID, op.RequestRef)
		if err != nil {
			return err
		}
		var command agentproto.Command
		if err = json.Unmarshal(body.Data, &command); err != nil {
			return err
		}
		currentSession = teamMCPReadOwnsSession(op, worker, command, incarnation) && worker.ContextID == view.ContextID
	}
	receipt := "stale"
	if !view.Invalidated && nav.Revision == view.TargetRevision && binding == view.Binding && currentSession {
		publication, err := tx.GetOperation(view.OperationID)
		if err != nil {
			return err
		}
		if publication.State == work.OperationPrepared || publication.State == work.OperationSending {
			return touchTeamPrivateNativeUI(tx, op, now)
		}
		if publication.State != work.OperationConfirmed || view.ExternalMessageID == "" {
			return finishTeamPrivateNativeUI(tx, op, "publication-unconfirmed", now)
		}
		result := view
		result.ID, result.ReplacesViewID = workui.ID(op.ID, "result"), view.ID
		result.BodyRef, result.OperationID, result.ExternalMessageID = "", "", ""
		card, err := workui.Card(result.ID, panel)
		if err != nil {
			return err
		}
		if _, err = workui.Queue(tx, result, card); err != nil {
			return err
		}
		receipt = result.ID
	}
	return finishTeamPrivateNativeUI(tx, op, receipt, now)
}

func teamMCPReadOwnsSession(op work.Operation, worker work.WorkerSession, command agentproto.Command, incarnation string) bool {
	return worker.ID == op.TargetID && worker.Generation == op.TargetGeneration && worker.State != work.WorkerRetired && worker.ContextID == op.ContextID && worker.NativeSessionID == command.Target.ThreadID && worker.CarrierIncarnation == incarnation && incarnation != ""
}

func touchTeamPrivateNativeUI(tx *workstore.Tx, op work.Operation, now time.Time) error {
	previous := op.Revision
	op.Revision++
	op.UpdatedAt = now
	_, _, err := tx.PutOperation(op, previous)
	return err
}

func finishTeamPrivateNativeUI(tx *workstore.Tx, op work.Operation, receipt string, now time.Time) error {
	if op.Kind == "private.goal.intent-ui" && op.State != work.OperationTerminal {
		op.State, op.ExternalReceipt = work.OperationConfirmed, receipt
	}
	op.Steps = append(op.Steps, work.OperationStep{ID: "ui_result", State: work.OperationConfirmed, Receipt: receipt})
	return touchTeamPrivateNativeUI(tx, op, now)
}
