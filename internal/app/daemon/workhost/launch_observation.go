package workhost

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (h *Host) confirmObservedLaunch(ctx context.Context, worker work.WorkerSession, operation *work.Operation) error {
	var identity agentproto.RuntimeProcessIdentity
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		identity, e = tx.GetCarrierProcess(worker.CarrierIncarnation)
		return e
	})
	if err != nil {
		return fmt.Errorf("worker launch %s requires reconciliation; automatic resend is forbidden: %w", operation.State, err)
	}
	actual, err := hostinventory.CaptureProcess(identity.PID)
	if err != nil || actual.Platform != identity.Platform || actual.StartIdentity != identity.StartIdentity {
		return fmt.Errorf("previous carrier launch has no matching live OS identity; automatic resend is forbidden")
	}
	receipt, _ := json.Marshal(identity)
	return h.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetOperation(operation.ID)
		if e != nil {
			return e
		}
		if current.Revision != operation.Revision {
			return workstore.ErrConflict
		}
		current.State = work.OperationConfirmed
		current.Revision++
		current.UpdatedAt = h.config.Now()
		current.ExternalReceipt = string(receipt)
		current.LastError = ""
		_, _, e = tx.PutOperation(current, operation.Revision)
		if e == nil {
			*operation = current
		}
		return e
	})
}
