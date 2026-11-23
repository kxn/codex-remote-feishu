package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func (p *privatePublisher) prepare(ctx context.Context, o privatechat.Owner, generation int64, id string) error {
	predecessor, err := p.journal.ResolveDisplayPredecessor(ctx, o, generation, id)
	if err != nil {
		return err
	}
	var event eventcontract.Event
	var preview *previewpkg.FinalBlockPreviewRequest
	var retry *previewpkg.FinalBlockPreviewRequest
	err = p.processor.InspectEffect(ctx, o, generation, id, func(app *privatechat.App, e privatechat.EffectRecord) error {
		if err := (privateHostReducer{p.host}).check(app); err != nil {
			return err
		}
		if e.Effect.Lane != "interactive" {
			return fmt.Errorf("native/daemon command is not a publication")
		}
		var err error
		event, err = privatechat.DecodeEffectEvent(e.Effect.Body)
		if err != nil {
			return err
		}
		if event.Command != nil || event.DaemonCommand != nil || event.SurfaceSessionID != o.SurfaceID {
			return privatechat.ErrJournalConflict
		}
		event = p.host.enrichTemporarySessionEventLocked(event)
		event.DaemonLifecycleID = p.host.daemonLifecycleID
		if event.Snapshot != nil {
			p.host.populateSnapshotFeishuPermissionGaps(event.Snapshot, o.SurfaceID)
		}
		if p.host.finalBlockPreviewer != nil && event.Kind == eventcontract.KindBlockCommitted && event.Block != nil && !event.InlineReplaceCurrentCard && event.Meta.MessageDelivery.TargetMessageID == "" {
			preview = &previewpkg.FinalBlockPreviewRequest{GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, WorkspaceRoot: p.host.previewWorkspaceRoot(o.SurfaceID, *event.Block), ThreadCWD: p.host.previewThreadCWD(o.SurfaceID, *event.Block), PreviewGrantKey: p.host.previewGrantKey(o.GatewayID, o.SurfaceID, *event.Block), TurnDiffSnapshot: event.TurnDiffSnapshot, Block: *event.Block}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if predecessor != nil && event.ExecCommandProgress != nil {
		if err = resolvePrivateProgressPublication(&event, *predecessor); err != nil {
			return err
		}
	}
	if preview != nil {
		previewCtx, cancel := p.host.newTimeoutContext(ctx, p.host.finalPreviewTimeout)
		result, previewErr := p.host.finalBlockPreviewer.RewriteFinalBlock(previewCtx, *preview)
		cancel()
		// The legacy preview service can succeed for one file or the diff while
		// another upload fails. Preserve those successful links as well as the
		// answer; only a missing or unrelated result is discarded.
		if result.Block.ID == preview.Block.ID && result.Block.Kind == preview.Block.Kind && result.Block.Text != "" {
			event.Block = &result.Block
			event.TurnDiffPreview = result.TurnDiffPreview
		}
		if previewErr != nil {
			if event.Block.Final {
				retry = preview
			}
			log.Printf("private final preview incomplete: surface=%s block=%s err=%v", o.SurfaceID, preview.Block.ID, previewErr)
		}
	}
	event.GatewayID = o.GatewayID
	event.Payload = nil
	event = event.Normalized()
	var operations []json.RawMessage
	err = p.processor.InspectEffect(ctx, o, generation, id, func(app *privatechat.App, _ privatechat.EffectRecord) error {
		if err := (privateHostReducer{p.host}).check(app); err != nil {
			return err
		}
		ops := p.host.projector.ProjectEvent(o.ChatID, event)
		ops = p.host.decorateReviewOperationsLocked(event, ops)
		replaceID := event.Meta.MessageDelivery.TargetMessageID
		if replaceID == "" && event.InlineReplaceCurrentCard {
			replaceID = event.SourceMessageID
		}
		if replaceID != "" {
			for i := range ops {
				if ops[i].Kind == feishu.OperationSendCard {
					ops[i].Kind = feishu.OperationUpdateCard
					ops[i].MessageID = replaceID
					ops[i].ReplyToMessageID = ""
					break
				}
			}
		}

		for i, op := range ops {
			op.GatewayID = o.GatewayID
			op.SurfaceSessionID = o.SurfaceID
			op.ChatID = o.ChatID
			op.ReceiveID, op.ReceiveIDType = feishu.ResolveReceiveTarget(o.ChatID, o.UserID)
			if op.Kind == feishu.OperationRemoveReaction {
				if err := p.resolveReaction(ctx, o, generation, id, &op); err != nil {
					return err
				}
			}
			digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", id, i)))
			raw, err := feishu.FreezePrivateOperation(op, hex.EncodeToString(digest[:20]))
			if err != nil {
				return err
			}
			operations = append(operations, raw)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if p.ownership != nil {
		for i, op := range operations {
			operations[i], err = p.ownership.PrepareOperation(ctx, o, generation, id, i, op)
			if err != nil {
				return err
			}
		}
	}
	plan, err := encodePrivatePublicationPlan(event, retry)
	if err != nil {
		return err
	}
	return p.journal.PreparePublication(ctx, o, generation, id, plan, operations)
}

func resolvePrivateProgressPublication(event *eventcontract.Event, predecessor privatechat.PublicationReceipt) error {
	previous, _, err := decodePrivatePublicationPlan(predecessor.Plan)
	if err != nil {
		return err
	}
	if event.ExecCommandProgress == nil || previous.ExecCommandProgress == nil || event.SurfaceSessionID != previous.SurfaceSessionID {
		return privatechat.ErrJournalConflict
	}
	current, prior := *event.ExecCommandProgress, *previous.ExecCommandProgress
	if current.ThreadID != prior.ThreadID || current.TurnID != prior.TurnID {
		return privatechat.ErrJournalConflict
	}
	// A predecessor with no visible operation carries its already-resolved
	// target forward; it cannot invent a new platform message.
	current.ActiveSegmentID = prior.ActiveSegmentID
	current.Segments = append([]control.ExecCommandProgressSegment(nil), prior.Segments...)
	for _, item := range predecessor.Results {
		var receipt feishu.PrivateOperationReceipt
		if err = json.Unmarshal(item.Receipt, &receipt); err != nil {
			return err
		}
		if receipt.Kind != feishu.OperationSendCard && receipt.Kind != feishu.OperationUpdateCard {
			continue
		}
		current, err = orchestrator.ResolvePrivateProgressDisplayTarget(current, prior, receipt.MessageID, receipt.ProgressCardStartSeq, receipt.ProgressCardEndSeq)
		if err != nil {
			return err
		}
		prior = current
	}
	event.ExecCommandProgress = &current
	event.Payload = nil
	return nil
}
