package workruntime

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (c *Coordinator) applyInputAdmissionReceipt(tx *workstore.Tx, binding agentproto.ExecutionBinding, event agentproto.Event) error {
	if event.CommandID == "" || event.RequestID == "" || event.Journal == nil {
		return fmt.Errorf("native input admission lacks exact RPC evidence")
	}
	source, err := reviewSource(tx, binding, event)
	if err != nil {
		return err
	}
	if event.Kind == agentproto.EventNativeTurnInputAccepted {
		return tx.AcceptAttemptInputAdmission(event.CommandID, source)
	}
	return tx.RejectAttemptInputAdmission(event.CommandID, source)
}

// Inspection of an uncertain steer may settle occupancy, never input delivery.
func (c *Coordinator) resolveStoppedInputAdmissions(tx *workstore.Tx, request InspectionRequest, observation NativeObservation) error {
	pending, err := tx.HasPendingAttemptInputAdmissions(request.Binding.AttemptID)
	if err != nil || !pending {
		return err
	}
	if !observation.provesStopped(request) {
		return fmt.Errorf("pending native input admission requires verified three-domain stop")
	}
	proof, err := c.persistQuiescenceEvidence(tx, request, observation)
	if err != nil {
		return err
	}
	return tx.ResolveUncertainAttemptInputAdmissions(request.Binding.AttemptID, proof)
}
