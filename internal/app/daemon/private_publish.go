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
	"github.com/kxn/codex-remote-feishu/internal/core/control"
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
	ValidateOperation(context.Context, privatechat.Owner, int64, string, int, json.RawMessage) error
	ConfirmOperation(context.Context, privatechat.Owner, int64, string, int) error
}

type privatePublicationGatewaySelector interface {
	PrivatePublicationGateway(gatewayID, appID string) (feishu.Gateway, error)
}

func (p *privatePublisher) Publish(ctx context.Context, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	if p == nil || p.host == nil || p.processor == nil || p.journal == nil || p.host.gateway == nil {
		return nil, fmt.Errorf("private publisher not configured")
	}
	if err := p.journal.PublicationOrderReady(ctx, o, generation, id); err != nil {
		return nil, err
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
		effect, e := p.journal.LoadEffect(ctx, o, generation, id)
		if e != nil {
			return nil, e
		}
		var local privatechat.PublicationReceipt
		if json.Unmarshal(effect.Receipt, &local) == nil && local.Version == 2 && local.Kind == "suppressed" {
			return p.processor.ApplyHostReceipt(ctx, o, generation, id, privateHostReceiptReducer{p.host})
		}
		if err = p.confirmOwnership(ctx, o, generation, publication); err != nil {
			return nil, err
		}
		return p.processor.ApplyHostReceipt(ctx, o, generation, id, privateHostReceiptReducer{p.host})
	}
	if publication.EffectState != "prepared" {
		// A committed per-operation begin marks the actual uncertainty boundary.
		// Confirmed operations are never replayed; prepared operations have not
		// reached the platform and may continue after the preceding receipt.
		for _, op := range publication.Operations {
			if op.State != "confirmed" && op.State != "prepared" {
				return nil, errPrivatePublicationUnknown
			}
		}
	}
	started := false
	start := func() error {
		if started {
			return nil
		}
		started = true
		if publication.EffectState == "prepared" {
			return p.journal.MarkEffectSending(ctx, o, generation, id)
		}
		return p.journal.ResumePublication(ctx, o, generation, id)
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
		planned, _, e := decodePrivatePublicationPlan(publication.Plan)
		if e != nil {
			return nil, e
		}
		if planned.PageView != nil && planned.PageView.PrivateModelResult {
			predecessor, e := p.journal.ResolveDisplayPredecessor(ctx, o, generation, id)
			if e != nil {
				return nil, e
			}
			stale, e := p.modelPublicationTarget(ctx, o, generation, id, &planned, predecessor)
			if e != nil {
				return nil, e
			}
			if stale {
				if e = start(); e != nil {
					return nil, e
				}
				if e = p.journal.SuppressPreparedPublication(ctx, o, generation, id, "stale_model_view", time.Now().UTC()); e != nil {
					return nil, e
				}
				return p.processor.ApplyHostReceipt(ctx, o, generation, id, privateHostReceiptReducer{p.host})
			}
		}
		operation, err := feishu.ThawPrivateOperation(op.Body)
		if err != nil {
			return nil, err
		}
		if operation.GatewayID != o.GatewayID || operation.SurfaceSessionID != o.SurfaceID || operation.ChatID != o.ChatID {
			return nil, privatechat.ErrJournalConflict
		}
		if operation.Kind == feishu.OperationUpdateCard {
			if err = p.journal.CardPublicationOrderReady(ctx, o, generation, id, operation.MessageID); err != nil {
				return nil, err
			}
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
		// Capture the exact application's adapter before starting a durable
		// operation. A worker replacement cannot redirect the captured call.
		gateway := p.host.gateway
		if selector, ok := gateway.(privatePublicationGatewaySelector); ok {
			gateway, err = selector.PrivatePublicationGateway(o.GatewayID, o.AppID)
			if err != nil {
				return nil, err
			}
		}
		if p.ownership != nil {
			if err = p.ownership.ValidateOperation(ctx, o, generation, id, op.Ordinal, op.Body); err != nil {
				return nil, err
			}
		}
		if err = start(); err != nil {
			return nil, err
		}
		begun := false
		if planned.TimelineText != nil && planned.TimelineText.Type == control.TimelineTextRecoveryInputWaiting {
			var suppressed bool
			begun, suppressed, err = p.beginRecoveryNotice(ctx, o, generation, id, op.Ordinal)
			if err != nil {
				return nil, err
			}
			if suppressed {
				return p.processor.ApplyHostReceipt(ctx, o, generation, id, privateHostReceiptReducer{p.host})
			}
		}
		if !begun {
			err = p.journal.BeginPublicationOperation(ctx, o, generation, id, op.Ordinal)
		}
		if err != nil {
			return nil, err
		}
		batch := []feishu.Operation{operation}
		applyCtx, cancel := p.host.newTimeoutContext(ctx, p.host.gatewayApplyTimeout)
		sendErr := gateway.Apply(applyCtx, batch)
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
	if err = start(); err != nil {
		return nil, err
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
