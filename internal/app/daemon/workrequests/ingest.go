// Package workrequests preserves native approval/input requests separately from
// project acceptance. A human decision is applied only to its original carrier.
package workrequests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func requestID(incarnation, id string) string {
	raw, _ := json.Marshal([]string{incarnation, id})
	hash := sha256.Sum256(raw)
	return "native_request_" + hex.EncodeToString(hash[:20])
}

// Apply is invoked inside the coordinator's journal acceptance transaction.
func Apply(tx *workstore.Tx, carrier workstore.CarrierRegistration, binding agentproto.ExecutionBinding, event agentproto.Event) error {
	if event.RequestID == "" || event.Journal == nil || binding.ValidateExecution() != nil {
		return fmt.Errorf("native request lacks causal execution evidence")
	}
	if event.Kind != agentproto.EventRequestStarted && event.Kind != agentproto.EventRequestResolved {
		return fmt.Errorf("not a native request event")
	}
	id := requestID(carrier.Incarnation, event.RequestID)
	current, err := tx.GetNativeRequest(id)
	expected := int64(0)
	if errors.Is(err, workstore.ErrNotFound) {
		current = workstore.NativeRequest{ID: id, ScopeID: binding.ScopeID, CarrierIncarnation: carrier.Incarnation, Binding: binding, NativeRequestID: event.RequestID, NativeSessionID: event.ThreadID, NativeTurnID: event.TurnID, State: "open", Revision: 1, CreatedAt: time.Now().UTC()}
	} else if err != nil {
		return err
	} else {
		if current.Binding != binding || current.NativeSessionID != event.ThreadID || current.NativeTurnID != event.TurnID {
			return workstore.ErrIdempotencyConflict
		}
		expected = current.Revision
		current.Revision++
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	evidence := work.NewContent(fmt.Sprintf("native_request_event_%s_%d", carrier.Incarnation, event.Journal.Sequence), binding.ScopeID, "application/json", raw, time.Now().UTC())
	if err = tx.PutContent(evidence); err != nil {
		return err
	}
	worker, err := tx.GetWorker(binding.WorkerSessionID)
	if err != nil {
		return err
	}
	current.UpdatedAt = time.Now().UTC()
	if event.Kind == agentproto.EventRequestStarted {
		if current.PromptRef == "" {
			current.PromptRef = evidence.ID
		} else {
			prior, e := tx.GetContent(current.ScopeID, current.PromptRef)
			if e != nil {
				return e
			}
			var original agentproto.Event
			decoder := json.NewDecoder(bytes.NewReader(prior.Data))
			decoder.UseNumber()
			if e = decoder.Decode(&original); e != nil {
				return e
			}
			before, e := requestSemantics(original)
			if e != nil {
				return e
			}
			after, e := requestSemantics(event)
			if e != nil {
				return e
			}
			if !bytes.Equal(before, after) {
				// Preserve both receipts but never approve changed content from
				// the old card. A new native request identity is required.
				if current.State != "resolved" {
					current.State = "stale"
				}
			}
		}
		if !carrier.Matches(worker) && current.State != "resolved" {
			current.State = "stale"
		}
	} else {
		if current.State != "stale" {
			current.State = "resolved"
		}
		current.ResolvedEvidenceRef = evidence.ID
		if current.ResponseOperationID != "" {
			op, e := tx.GetOperation(current.ResponseOperationID)
			if e != nil {
				return e
			}
			if op.State != work.OperationConfirmed {
				previous := op.Revision
				op.Revision++
				op.State = work.OperationConfirmed
				op.ExternalReceipt = evidence.ID
				op.UpdatedAt = current.UpdatedAt
				if _, _, e = tx.PutOperation(op, previous); e != nil {
					return e
				}
			}
		}
	}
	return tx.PutNativeRequest(current, expected)
}

func requestSemantics(event agentproto.Event) ([]byte, error) {
	return json.Marshal(struct {
		Prompt   *agentproto.RequestPrompt
		Metadata map[string]any
	}{event.RequestPrompt, event.Metadata})
}
