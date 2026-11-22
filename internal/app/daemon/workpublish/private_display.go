package workpublish

import (
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (p *Publisher) resolvePrivateDisplay(tx *workstore.Tx, op work.Operation, item work.Outbox, job *publication, recovery bool) error {
	projection := &job.Projection
	meta := projection.PrivateDisplay
	if meta == nil {
		return nil
	}
	if op.Actor != (work.Actor{Kind: work.ActorSystem, ID: "private-execution-display"}) || projection.UIViewID != "" || projection.TaskID != "" || projection.RootMessageID != "" || projection.ReplaceMessageID != "" {
		return workstore.ErrScopeDenied
	}
	display, err := tx.VerifyPrivateDisplayPublication(op)
	if err != nil || meta.SourceIngressID != "" {
		return err
	}
	if recovery && (meta.AnchorOutboxID != "" || display.LastOutboxID != item.ID) {
		return fmt.Errorf("private execution output cannot be replayed after a later output or uncertainty in its card update")
	}
	if err := p.waitForPrivatePredecessor(tx, op, meta); err != nil {
		return err
	}
	if meta.AnchorOutboxID == "" {
		return nil
	}

	anchor, anchorMeta, err := privateDisplayPredecessor(tx, op, meta.DisplayID, meta.AnchorOutboxID)
	if err != nil {
		return err
	}
	if anchorMeta.AnchorOutboxID != "" || !anchorMeta.Progress {
		return workstore.ErrScopeDenied
	}
	switch anchor.State {
	case work.OutboxPending:
		return workstore.ErrConflict
	case work.OutboxSending:
		if p.privateSendMayBeLive(tx, anchor) {
			return workstore.ErrConflict
		}
	case work.OutboxRetryable:
		if _, err := tx.SupersedePrivateDisplayUpdate(anchor.ID, p.now()); err != nil {
			return err
		}
	}
	if anchor.State != work.OutboxSent || anchor.ExternalMessageID == "" {
		job.TerminalReason = "the private progress card has no confirmed message"
		return nil
	}
	if meta.PatchFenceID != "" && meta.PatchFenceID != meta.AnchorOutboxID {
		fence, _, err := privateDisplayPredecessor(tx, op, meta.DisplayID, meta.PatchFenceID)
		if err != nil {
			return err
		}
		switch fence.State {
		case work.OutboxPending, work.OutboxRetryable:
			return workstore.ErrConflict
		case work.OutboxSending:
			if p.privateSendMayBeLive(tx, fence) {
				return workstore.ErrConflict
			}
			fallthrough
		case work.OutboxUnknown, work.OutboxTerminal:
			job.TerminalReason = "the previous private progress update was not confirmed"
			return nil
		}
	}
	projection.ReplaceMessageID = anchor.ExternalMessageID
	return nil
}

func (p *Publisher) waitForPrivatePredecessor(tx *workstore.Tx, op work.Operation, meta *workservice.PrivateDisplayPublication) error {
	if meta.PreviousOutboxID == "" {
		return nil
	}
	previous, previousMeta, err := privateDisplayPredecessor(tx, op, meta.DisplayID, meta.PreviousOutboxID)
	if err != nil {
		return err
	}
	switch previous.State {
	case work.OutboxPending:
		return workstore.ErrConflict
	case work.OutboxRetryable:
		if !previousMeta.Progress {
			return workstore.ErrConflict
		}
		_, err := tx.SupersedePrivateDisplayUpdate(previous.ID, p.now())
		return err
	case work.OutboxSending:
		if p.privateSendMayBeLive(tx, previous) {
			return workstore.ErrConflict
		}
	}
	return nil
}

func (p *Publisher) privateSendMayBeLive(tx *workstore.Tx, item work.Outbox) bool {
	op, err := tx.GetOperation(item.OperationID)
	return err != nil || p.now().Sub(op.UpdatedAt) < 2*time.Minute
}

func privateDisplayPredecessor(tx *workstore.Tx, current work.Operation, displayID, id string) (work.Outbox, *workservice.PrivateDisplayPublication, error) {
	item, err := tx.GetOutbox(id)
	if err != nil {
		return item, nil, err
	}
	op, err := tx.GetOperation(item.OperationID)
	if err != nil {
		return item, nil, err
	}
	if op.ID == current.ID || item.ScopeID != current.ScopeID || op.ContextID != current.ContextID || op.Actor != current.Actor || op.TargetID != current.TargetID || op.TargetGeneration != current.TargetGeneration {
		return item, nil, workstore.ErrScopeDenied
	}
	projection, err := workservice.ReadMessageProjection(tx, op)
	if err != nil {
		return item, nil, err
	}
	meta := projection.PrivateDisplay
	if meta == nil || meta.DisplayID != displayID {
		return item, nil, workstore.ErrScopeDenied
	}
	return item, meta, nil
}
