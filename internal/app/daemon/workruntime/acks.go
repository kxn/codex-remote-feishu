package workruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workdisplay"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (c *Coordinator) validateTransport(tx *workstore.Tx, carrier workstore.CarrierRegistration, instanceID string) error {
	if instanceID == carrier.OriginInstanceID {
		return nil
	}
	current, err := tx.ListCarriersForInstance(instanceID)
	if err != nil {
		return err
	}
	for _, registration := range current {
		if registration.Owner.WorkerSessionID == carrier.Owner.WorkerSessionID && registration.Owner.ScopeID == carrier.Owner.ScopeID && registration.Owner.ContextID == carrier.Owner.ContextID && registration.Owner.RoleID == carrier.Owner.RoleID {
			return nil
		}
	}
	return fmt.Errorf("event transport does not belong to registered worker history")
}

type CommittedAck struct {
	coordinator *Coordinator
	attemptID   string
}

func (c *Coordinator) CommitAck(ctx context.Context, currentTransportInstance string, ack agentproto.CommandAck) (*CommittedAck, error) {
	var attemptID string
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		command, err := tx.GetNativeCommand(ack.CommandID)
		if err != nil {
			return err
		}
		carrier, err := tx.GetCarrier(command.Incarnation)
		if err != nil {
			return err
		}
		if err := c.validateTransport(tx, carrier, currentTransportInstance); err != nil {
			return err
		}
		if ack.RuntimeReceipt == nil || ack.RuntimeReceipt.Version != agentproto.JournalProtocolVersion || ack.RuntimeReceipt.CommandID != command.CommandID || ack.RuntimeReceipt.CarrierIncarnation != command.Incarnation {
			return fmt.Errorf("command ACK lacks exact journal identity")
		}
		canonical, err := tx.NativeCommandPayload(command.CommandID, command.Incarnation)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(canonical)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(raw)
		if hex.EncodeToString(hash[:]) != ack.RuntimeReceipt.PayloadHash {
			return fmt.Errorf("command ACK payload hash mismatch")
		}
		if canonical.Kind == agentproto.CommandThreadShellCommand && canonical.ShellCommand.OperationID != "" && canonical.IsOwnerOnlyRuntimeCommand() {
			if err := tx.ApplyPrivateShellAck(ack, c.options.Clock()); err != nil {
				return err
			}
			saved, err := tx.GetPrivateShellOperation(ack.CommandID)
			if err != nil {
				return err
			}
			return workdisplay.ProjectPrivateShellStatus(tx, saved, c.options.Clock())
		}
		if err := tx.RecordNativeAck(command.CommandID, ack); err != nil {
			return err
		}
		worker, err := tx.GetWorker(command.Binding.WorkerSessionID)
		if err != nil {
			return err
		}
		if !carrier.Matches(worker) {
			return nil
		}
		if !ack.Accepted {
			attemptID = command.Binding.AttemptID
			return c.gate(tx, carrier, attemptID, "native_command_rejected")
		}
		if ack.RuntimeReceipt.Stage == "unknown" {
			return c.gate(tx, carrier, command.Binding.AttemptID, "native_command_unknown")
		}
		// Accepted/prepared or written only describes transport, not execution.
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &CommittedAck{coordinator: c, attemptID: attemptID}, nil
}
func (c *Coordinator) PublishCommittedAck(ctx context.Context, ack *CommittedAck) error {
	if ack == nil || ack.coordinator != c {
		return fmt.Errorf("ack commit token required")
	}
	if ack.attemptID != "" {
		return c.scheduleReconcile(ctx, ack.attemptID)
	}
	return nil
}
func (c *Coordinator) HandleAck(ctx context.Context, instance string, ack agentproto.CommandAck) error {
	committed, err := c.CommitAck(ctx, instance, ack)
	if err != nil {
		return err
	}
	return c.PublishCommittedAck(ctx, committed)
}
