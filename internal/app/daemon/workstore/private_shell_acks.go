package workstore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Only the exact journal's rejected prepared-zero frame inventory proves a
// registered command never reached native stdin. Other ACKs cannot consume or
// release the original input and never create an empty model-attempt gate.
func (t *Tx) ApplyPrivateShellAck(ack agentproto.CommandAck, now time.Time) error {
	return t.atomic(func() error {
		v, err := t.GetPrivateShellOperation(ack.CommandID)
		if err != nil {
			return err
		}
		native, err := t.GetNativeCommand(v.CommandID)
		if err != nil {
			return err
		}
		command, err := t.NativeCommandPayload(v.CommandID, v.Incarnation)
		if err != nil {
			return err
		}
		receipt := ack.RuntimeReceipt
		raw, err := json.Marshal(command)
		if err != nil {
			return err
		}
		if command.Kind != agentproto.CommandThreadShellCommand || command.ShellCommand.OperationID != v.ID || !command.IsOwnerOnlyRuntimeCommand() || command.Execution == nil || *command.Execution != v.Execution || native.Binding != v.Execution || native.Incarnation != v.Incarnation || receipt == nil || receipt.Version != agentproto.JournalProtocolVersion || receipt.CommandID != v.CommandID || receipt.CarrierIncarnation != v.Incarnation || receipt.PayloadHash != work.NewContent("hash", v.ScopeID, "application/json", raw, now).SHA256 {
			return ErrScopeDenied
		}
		zero := !ack.Accepted && receipt.Stage == "prepared" && receipt.PreparedFrames == 0 && receipt.WrittenFrames == 0
		priorFrames := native.Ack != nil && native.Ack.RuntimeReceipt != nil && (native.Ack.RuntimeReceipt.PreparedFrames != 0 || native.Ack.RuntimeReceipt.WrittenFrames != 0 || native.Ack.RuntimeReceipt.Stage != "prepared")
		if priorFrames && receipt.PreparedFrames == 0 && receipt.WrittenFrames == 0 {
			// A weaker replay must not erase the durable possibility of a write;
			// otherwise replaying that same zero-frame ACK twice could release it.
			if !ack.Accepted || receipt.Stage == "unknown" {
				return t.MarkPrivateShellOperationUnknown(v.ID, "conflicting native shell receipt; prior native write evidence retained")
			}
			return nil
		}
		if err = t.RecordNativeAck(v.CommandID, ack); err != nil {
			return err
		}
		if !zero || priorFrames || v.ItemID != "" || v.State == "running" || v.State == "completed" {
			if !ack.Accepted || receipt.Stage == "unknown" {
				return t.MarkPrivateShellOperationUnknown(v.ID, "native shell receipt does not prove delivery: "+ack.Error)
			}
			return nil
		}
		if v.State == "rejected" {
			return nil
		}
		proofRaw, err := json.Marshal(ack)
		if err != nil {
			return err
		}
		proof := work.NewContent(fmt.Sprintf("private-shell-unsent:%s:%d", v.ID, v.Revision), v.ScopeID, "application/vnd.codex-remote.private-shell-prepared-zero+json", proofRaw, now)
		if err = t.PutContent(proof); err != nil {
			return err
		}
		before := v.Revision
		v.Revision++
		v.State = "rejected"
		v.ReceiptRef = proof.ID
		v.LastError = ack.Error
		v.UpdatedAt = now
		if err = t.savePrivateShellOperation(v, before); err != nil {
			return err
		}
		op, err := t.GetOperation(v.CommandID)
		if err != nil {
			return err
		}
		old := op.Revision
		op.Revision++
		op.State = work.OperationTerminal
		op.UpdatedAt = now
		op.LastError = ack.Error
		op.Steps = append(op.Steps, work.OperationStep{ID: "native_prepared_zero_rejection", State: work.OperationConfirmed, Receipt: proof.ID})
		if _, _, err = t.PutOperation(op, old); err != nil {
			return err
		}
		return t.releasePrivateShellInputs(v)
	})
}
