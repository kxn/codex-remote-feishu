package workstore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Actual UserShell receipts belong to their own session-level operation. Native
// turn IDs can differ from the model turn used only to authorize sending.
func (t *Tx) ApplyPrivateShellEvent(event agentproto.Event, now time.Time) (v PrivateShellOperation, err error) {
	err = t.atomic(func() error {
		var e error
		v, e = t.GetPrivateShellOperation(event.CommandID)
		if e != nil {
			return e
		}
		if event.Journal == nil || event.Execution == nil || *event.Execution != v.Execution || event.Journal.CarrierIncarnation != v.Incarnation || event.ThreadID != v.SessionID || (event.Kind != agentproto.EventNativeShellRPCResult && event.Kind != agentproto.EventNativeShellItemObserved) {
			return ErrScopeDenied
		}
		saved, e := t.GetRuntimeEvent(v.Incarnation, event.Journal.Sequence)
		if e != nil {
			return e
		}
		savedHash, e := agentproto.RuntimeEventPayloadHash(saved)
		if e != nil {
			return e
		}
		incomingHash, e := agentproto.RuntimeEventPayloadHash(event)
		if e != nil {
			return e
		}
		if saved.Journal == nil || *saved.Journal != *event.Journal || savedHash != event.Journal.PayloadHash || incomingHash != savedHash {
			return ErrScopeDenied
		}
		native, e := t.GetNativeCommand(v.CommandID)
		if e != nil {
			return e
		}
		if native.Binding != v.Execution || native.Incarnation != v.Incarnation || native.Kind != agentproto.CommandThreadShellCommand {
			return ErrScopeDenied
		}
		command, e := t.NativeCommandPayload(v.CommandID, v.Incarnation)
		if e != nil {
			return e
		}
		if command.ShellCommand.OperationID != v.ID || command.Execution == nil || *command.Execution != v.Execution || command.NativeInputAdmission != nil {
			return ErrScopeDenied
		}
		if event.Kind == agentproto.EventNativeShellRPCResult && (event.RequestID == "" || event.TurnID != "") {
			return ErrScopeDenied
		}
		if event.Kind == agentproto.EventNativeShellItemObserved && (event.TurnID == "" || event.ItemID == "" || (event.Action != "started" && event.Action != "completed")) {
			return ErrScopeDenied
		}
		raw, e := json.Marshal(event)
		if e != nil {
			return e
		}
		content := work.NewContent(fmt.Sprintf("private-shell-receipt:%s:%d", v.Incarnation, event.Journal.Sequence), v.ScopeID, "application/vnd.codex-remote.private-shell-receipt+json", raw, now)
		if e = t.PutContent(content); e != nil {
			return e
		}
		if v.State == "completed" {
			if event.Kind == agentproto.EventNativeShellItemObserved && (event.ItemID != v.ItemID || event.TurnID != v.ActualTurnID) {
				return ErrScopeDenied
			}
			return nil
		}
		if v.State == "rejected" {
			return ErrConflict
		}
		before := v.Revision
		v.Revision++
		v.UpdatedAt = now
		v.LastError = event.ErrorMessage
		op, e := t.GetOperation(v.CommandID)
		if e != nil {
			return e
		}
		old := op.Revision
		op.Revision++
		op.UpdatedAt = now
		op.LastError = event.ErrorMessage
		switch event.Kind {
		case agentproto.EventNativeShellRPCResult:
			switch event.Status {
			case "acknowledged":
				v.Acknowledged = true
				if v.State != "running" {
					v.State = "unknown"
				}
			case "unknown":
				if v.State != "running" {
					v.State = "unknown"
				}
			case "failed":
				if v.State == "running" {
					return ErrConflict
				}
				v.State = "rejected"
			default:
				return ErrScopeDenied
			}
			if v.State == "rejected" {
				op.State = work.OperationTerminal
			} else {
				op.State = work.OperationUnknown
			}
		case agentproto.EventNativeShellItemObserved:
			if v.ItemID != "" && (v.ItemID != event.ItemID || v.ActualTurnID != event.TurnID) {
				return ErrScopeDenied
			}
			v.ItemID = event.ItemID
			v.ActualTurnID = event.TurnID
			v.ReceiptRef = content.ID
			var exit struct {
				ExitCode *int `json:"exitCode"`
			}
			metadata, _ := json.Marshal(event.Metadata)
			_ = json.Unmarshal(metadata, &exit)
			if event.Action == "completed" && event.Status == "completed" && exit.ExitCode != nil && *exit.ExitCode == 0 {
				v.State = "completed"
				op.State = work.OperationConfirmed
				op.ExternalReceipt = content.ID
			} else {
				v.State = "running"
				if event.Action == "completed" {
					v.State = "unknown"
				}
				op.State = work.OperationUnknown
			}
		}
		if e = t.savePrivateShellOperation(v, before); e != nil {
			return e
		}
		if _, _, e = t.PutOperation(op, old); e != nil {
			return e
		}
		if v.State == "rejected" {
			return t.releasePrivateShellInputs(v)
		}
		if v.State == "completed" {
			for _, id := range v.InputIDs {
				input, e := t.GetInbox(id)
				if e != nil {
					return e
				}
				if input.ShellOperationID != v.ID {
					return ErrConflict
				}
				if input.State == work.InboxCancelled {
					continue
				}
				if input.State != work.InboxPending {
					return ErrConflict
				}
				revision := input.Revision
				input.Revision++
				input.State = work.InboxProcessed
				input.ProcessedAt = &now
				input.ProcessingOutcomeRef = content.ID
				if e = t.PutInbox(input, revision); e != nil {
					return fmt.Errorf("consume actual shell input: %w", e)
				}
			}
		}
		return nil
	})
	return
}
