package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

const privatePreviewRetryKind control.DaemonCommandKind = "private.preview.retry"

type privatePublicationPlan struct {
	Version int                                  `json:"version"`
	Event   json.RawMessage                      `json:"event"`
	Retry   *previewpkg.FinalBlockPreviewRequest `json:"preview_retry,omitempty"`
}

func encodePrivatePublicationPlan(event eventcontract.Event, retry *previewpkg.FinalBlockPreviewRequest) (json.RawMessage, error) {
	raw, err := privatechat.EncodeEffectEvent(event)
	if err != nil || retry == nil {
		return raw, err
	}
	return json.Marshal(privatePublicationPlan{Version: 2, Event: raw, Retry: retry})
}
func decodePrivatePublicationPlan(raw json.RawMessage) (eventcontract.Event, *previewpkg.FinalBlockPreviewRequest, error) {
	var plan privatePublicationPlan
	if json.Unmarshal(raw, &plan) == nil && plan.Version == 2 {
		event, err := privatechat.DecodeEffectEvent(plan.Event)
		return event, plan.Retry, err
	}
	event, err := privatechat.DecodeEffectEvent(raw)
	return event, nil, err
}

type privatePreviewRetryIntent struct {
	Version   int
	Event     json.RawMessage
	Request   previewpkg.FinalBlockPreviewRequest
	MessageID string
}
type privatePreviewRetryReceipt struct {
	Version  int
	EffectID string
	Intent   privatePreviewRetryIntent
	Result   previewpkg.FinalBlockPreviewResult
	Skipped  bool
}

// The original final card is already confirmed when this effect is created.
// Failure to enrich its links cannot hold execution or cause another card send.
func privatePreviewRetryEvents(event eventcontract.Event, req *previewpkg.FinalBlockPreviewRequest, receipt privatechat.PublicationReceipt) ([]eventcontract.Event, error) {
	if req == nil {
		return nil, nil
	}
	if event.Kind != eventcontract.KindBlockCommitted || event.Block == nil || !event.Block.Final || req.SurfaceSessionID != event.SurfaceSessionID || req.GatewayID != event.GatewayID || req.Block.ID != event.Block.ID {
		return nil, privatechat.ErrJournalConflict
	}
	message := ""
	for _, item := range receipt.Results {
		op, err := feishu.ThawPrivateOperation(item.Operation)
		if err != nil {
			return nil, err
		}
		if op.Kind != feishu.OperationSendCard || op.FinalSourceBody() == "" {
			continue
		}
		var result feishu.PrivateOperationReceipt
		if err = json.Unmarshal(item.Receipt, &result); err != nil {
			return nil, err
		}
		message = result.MessageID
		break
	}
	if message == "" {
		return nil, nil
	}
	raw, err := privatechat.EncodeEffectEvent(event)
	if err != nil {
		return nil, err
	}
	copyReq := *req
	copyReq.Block = *event.Block
	intent, err := json.Marshal(privatePreviewRetryIntent{Version: 1, Event: raw, Request: copyReq, MessageID: message})
	if err != nil {
		return nil, err
	}
	cmd := control.DaemonCommand{Kind: privatePreviewRetryKind, GatewayID: event.GatewayID, SurfaceSessionID: event.SurfaceSessionID, Text: string(intent)}
	return []eventcontract.Event{{Kind: eventcontract.KindDaemonCommand, GatewayID: event.GatewayID, SurfaceSessionID: event.SurfaceSessionID, DaemonCommand: &cmd}}, nil
}

func runPrivatePreviewRetry(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != privatePreviewRetryKind || cmd.SurfaceSessionID != o.SurfaceID {
		return nil, privatechat.ErrJournalConflict
	}
	var intent privatePreviewRetryIntent
	if json.Unmarshal([]byte(cmd.Text), &intent) != nil || intent.Version != 1 || intent.MessageID == "" || intent.Request.GatewayID != o.GatewayID || intent.Request.SurfaceSessionID != o.SurfaceID || intent.Request.ChatID != o.ChatID || intent.Request.ActorUserID != o.UserID {
		return nil, privatechat.ErrJournalConflict
	}
	reducer := privatePreviewRetryReducer{host: host}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, reducer)
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, privatechat.ErrJournalState
	}
	result := privatePreviewRetryReceipt{Version: 1, EffectID: id, Intent: intent, Skipped: record.State == "unknown" || host.finalBlockPreviewer == nil}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	if !result.Skipped {
		previewCtx, cancel := host.newTimeoutContext(ctx, secondChanceFinalPreviewTimeout(host.finalPreviewTimeout))
		result.Result, err = host.finalBlockPreviewer.RewriteFinalBlock(previewCtx, intent.Request)
		cancel()
		// Preserve partial success just as the first preview attempt does.
		if result.Result.Block.ID != intent.Request.Block.ID || result.Result.Block.Kind != intent.Request.Block.Kind || result.Result.Block.Text == "" {
			result.Skipped = true
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = j.ConfirmLocalEffect(saveCtx, o, generation, id, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, reducer)
}

type privatePreviewRetryReducer struct{ host *App }

func (r privatePreviewRetryReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	var receipt privatePreviewRetryReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != effect.ID {
		return nil, privatechat.ErrJournalConflict
	}
	original, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	intentRaw, err := json.Marshal(receipt.Intent)
	if err != nil || original.DaemonCommand == nil || original.DaemonCommand.Text != string(intentRaw) {
		return nil, privatechat.ErrJournalConflict
	}
	if receipt.Skipped {
		return nil, nil
	}
	event, err := privatechat.DecodeEffectEvent(receipt.Intent.Event)
	if err != nil || event.Block == nil {
		return nil, fmt.Errorf("invalid preview source event: %w", err)
	}
	anchor := app.Service().LookupFinalCardForBlock(event.SurfaceSessionID, *event.Block, event.DaemonLifecycleID)
	if anchor == nil || anchor.MessageID != receipt.Intent.MessageID {
		return nil, nil
	}
	if sameFinalPatchResult(*event.Block, event.TurnDiffPreview, receipt.Result) {
		return nil, nil
	}
	event.Block = &receipt.Result.Block
	event.TurnDiffPreview = receipt.Result.TurnDiffPreview
	event.Meta.MessageDelivery = eventcontract.MessageDelivery{Mutation: eventcontract.MessageMutationPatchSameMessage, TargetMessageID: anchor.MessageID}
	event.Payload = nil
	return []eventcontract.Event{event.Normalized()}, nil
}
