package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

var errPrivatePublicationUnknown = errors.New("private platform operation requires delivery reconciliation")

// privatePublisher executes the old private projection, with durable individual
// operation results. It owns no core state: all observation/completion uses the
// same Processor as inbound and native events.
type privatePublisher struct {
	host      *App
	processor *privatechat.Processor
	journal   *privatechat.Journal
	ownership privatePublicationOwnership
}

// Implemented by the neutral ingress registry adapter. Confirmation reads the
// durable publication result; it must not trust a caller-supplied message ID.
type privatePublicationOwnership interface {
	PrepareOperation(context.Context, privatechat.Owner, int64, string, int, json.RawMessage) (json.RawMessage, error)
	ConfirmOperation(context.Context, privatechat.Owner, int64, string, int) error
}

func (p *privatePublisher) Publish(ctx context.Context, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	if p == nil || p.host == nil || p.processor == nil || p.journal == nil || p.host.gateway == nil {
		return nil, fmt.Errorf("private publisher not configured")
	}
	publication, err := p.journal.LoadPublication(ctx, o, generation, id)
	if errors.Is(err, sql.ErrNoRows) {
		if err = p.prepare(ctx, o, generation, id); err != nil {
			return nil, err
		}
		publication, err = p.journal.LoadPublication(ctx, o, generation, id)
	}
	if err != nil {
		return nil, err
	}
	if publication.EffectState == "confirmed" {
		if err = p.confirmOwnership(ctx, o, generation, publication); err != nil {
			return nil, err
		}
		return p.processor.ApplyHostReceipt(ctx, o, generation, id, privateHostReceiptReducer{p.host})
	}
	if publication.EffectState == "prepared" {
		if err = p.journal.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	} else {
		// A committed per-operation begin marks the actual uncertainty boundary.
		// Confirmed operations are never replayed; prepared operations have not
		// reached the platform and may continue after the preceding receipt.
		for _, op := range publication.Operations {
			if op.State != "confirmed" && op.State != "prepared" {
				return nil, errPrivatePublicationUnknown
			}
		}
		if err = p.journal.ResumePublication(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	for _, op := range publication.Operations {
		if op.State == "confirmed" {
			if p.ownership != nil {
				if err = p.ownership.ConfirmOperation(ctx, o, generation, id, op.Ordinal); err != nil {
					return nil, err
				}
			}
			continue
		}
		if op.State != "prepared" {
			return nil, errPrivatePublicationUnknown
		}
		operation, err := feishu.ThawPrivateOperation(op.Body)
		if err != nil {
			return nil, err
		}
		if operation.GatewayID != o.GatewayID || operation.SurfaceSessionID != o.SurfaceID || operation.ChatID != o.ChatID {
			return nil, privatechat.ErrJournalConflict
		}
		if operation.Kind == feishu.OperationAddReaction || operation.Kind == feishu.OperationRemoveReaction {
			held, e := p.journal.HasUncertainMigratedReaction(ctx, o, generation, operation.MessageID)
			if e != nil {
				return nil, e
			}
			if held {
				return nil, errPrivatePublicationUnknown
			}
		}
		if err = p.journal.BeginPublicationOperation(ctx, o, generation, id, op.Ordinal); err != nil {
			return nil, err
		}
		batch := []feishu.Operation{operation}
		applyCtx, cancel := p.host.newTimeoutContext(ctx, p.host.gatewayApplyTimeout)
		sendErr := p.host.gateway.Apply(applyCtx, batch)
		cancel()
		var receipt feishu.PrivateOperationReceipt
		if sendErr == nil {
			receipt, sendErr = feishu.PrivateReceiptForOperation(batch[0], time.Now().UTC())
		}
		if sendErr != nil {
			body, _ := json.Marshal(struct {
				Operation int
				Error     string
			}{op.Ordinal, sendErr.Error()})
			// A canceled request still needs a local durable unknown marker.
			saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			e1 := p.journal.CompletePublicationOperation(saveCtx, o, generation, id, op.Ordinal, "unknown", body, nil)
			e2 := p.journal.ResolveEffect(saveCtx, o, generation, id, "unknown", body, nil)
			saveCancel()
			return nil, errors.Join(errPrivatePublicationUnknown, sendErr, e1, e2)
		}
		raw, err := json.Marshal(receipt)
		if err != nil {
			return nil, err
		}
		verifier := privatePublicationResult{o: o, generation: generation, id: id, ordinal: op.Ordinal, body: op.Body, receipt: raw}
		saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = p.journal.CompletePublicationOperation(saveCtx, o, generation, id, op.Ordinal, "confirmed", raw, verifier)
		saveCancel()
		if err != nil {
			return nil, err
		}
		if p.ownership != nil {
			if err = p.ownership.ConfirmOperation(ctx, o, generation, id, op.Ordinal); err != nil {
				return nil, err
			}
		}
	}
	if _, err = p.journal.FinishPublication(ctx, o, generation, id, time.Now().UTC()); err != nil {
		return nil, err
	}
	return p.processor.ApplyHostReceipt(ctx, o, generation, id, privateHostReceiptReducer{p.host})
}

func (p *privatePublisher) confirmOwnership(ctx context.Context, o privatechat.Owner, generation int64, publication privatechat.Publication) error {
	if p.ownership == nil {
		return nil
	}
	for _, op := range publication.Operations {
		if op.State != "confirmed" {
			return privatechat.ErrJournalState
		}
		if err := p.ownership.ConfirmOperation(ctx, o, generation, publication.EffectID, op.Ordinal); err != nil {
			return err
		}
	}
	return nil
}

// The verifier is scoped to one actual successful gateway return. No external
// caller can submit a JSON receipt and have it recognized as a platform result.
type privatePublicationResult struct {
	o             privatechat.Owner
	generation    int64
	id            string
	ordinal       int
	body, receipt json.RawMessage
}

func (v privatePublicationResult) VerifyPublicationResult(_ context.Context, o privatechat.Owner, generation int64, id string, ordinal int, body, receipt json.RawMessage) error {
	if o != v.o || generation != v.generation || id != v.id || ordinal != v.ordinal || string(body) != string(v.body) || string(receipt) != string(v.receipt) {
		return privatechat.ErrJournalConflict
	}
	return nil
}
