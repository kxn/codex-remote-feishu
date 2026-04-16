package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

const minSecondChanceFinalPreviewTimeout = 500 * time.Millisecond

type secondChanceFinalPatchJob struct {
	GatewayID            string
	ChatID               string
	SurfaceSessionID     string
	DaemonLifecycleID    string
	SourceMessageID      string
	SourceMessagePreview string
	SentBlock            render.Block
	FileChangeSummary    *control.FileChangeSummary
	FinalTurnSummary     *control.FinalTurnSummary
	PreviewRequest       feishu.FinalBlockPreviewRequest
}

func (a *App) maybeScheduleSecondChanceFinalPatchLocked(gatewayID, chatID string, event control.UIEvent, previewReq feishu.FinalBlockPreviewRequest, rewriteErr error) {
	if a == nil || a.finalBlockPreviewer == nil || rewriteErr == nil || a.shuttingDown {
		return
	}
	if event.Kind != control.UIEventBlockCommitted || event.Block == nil || !event.Block.Final {
		return
	}
	if previewReq.Block.Kind != render.BlockAssistantMarkdown || strings.TrimSpace(previewReq.Block.Text) == "" {
		return
	}
	job := secondChanceFinalPatchJob{
		GatewayID:            strings.TrimSpace(gatewayID),
		ChatID:               strings.TrimSpace(chatID),
		SurfaceSessionID:     strings.TrimSpace(event.SurfaceSessionID),
		DaemonLifecycleID:    strings.TrimSpace(event.DaemonLifecycleID),
		SourceMessageID:      strings.TrimSpace(event.SourceMessageID),
		SourceMessagePreview: strings.TrimSpace(event.SourceMessagePreview),
		SentBlock:            *event.Block,
		PreviewRequest:       previewReq,
	}
	if event.FileChangeSummary != nil {
		summary := *event.FileChangeSummary
		if len(summary.Files) != 0 {
			summary.Files = append([]control.FileChangeSummaryEntry(nil), summary.Files...)
		}
		job.FileChangeSummary = &summary
	}
	if event.FinalTurnSummary != nil {
		summary := *event.FinalTurnSummary
		if summary.Usage != nil {
			usage := *summary.Usage
			summary.Usage = &usage
		}
		if summary.ThreadUsage != nil {
			usage := *summary.ThreadUsage
			summary.ThreadUsage = &usage
		}
		job.FinalTurnSummary = &summary
	}
	go a.runSecondChanceFinalPatch(job)
}

func (a *App) runSecondChanceFinalPatch(job secondChanceFinalPatchJob) {
	a.mu.Lock()
	if a.shuttingDown || a.finalBlockPreviewer == nil {
		a.mu.Unlock()
		return
	}
	previewer := a.finalBlockPreviewer
	projector := a.projector
	gateway := a.gateway
	previewTimeout := secondChanceFinalPreviewTimeout(a.finalPreviewTimeout)
	gatewayTimeout := a.gatewayApplyTimeout
	a.mu.Unlock()

	previewCtx, previewCancel := a.newTimeoutContext(context.Background(), previewTimeout)
	result, err := previewer.RewriteFinalBlock(previewCtx, job.PreviewRequest)
	previewCancel()
	if err != nil {
		log.Printf(
			"second-chance final patch preview rewrite failed: surface=%s thread=%s turn=%s item=%s err=%v",
			job.SurfaceSessionID,
			job.PreviewRequest.Block.ThreadID,
			job.PreviewRequest.Block.TurnID,
			job.PreviewRequest.Block.ItemID,
			err,
		)
		return
	}
	if sameFinalPatchBlock(job.SentBlock, result.Block) {
		return
	}

	a.mu.Lock()
	if a.shuttingDown {
		a.mu.Unlock()
		return
	}
	anchor := a.service.LookupFinalCardForBlock(job.SurfaceSessionID, job.SentBlock, job.DaemonLifecycleID)
	a.mu.Unlock()
	if anchor == nil {
		return
	}

	ops := projector.Project(job.ChatID, control.UIEvent{
		Kind:                 control.UIEventBlockCommitted,
		GatewayID:            job.GatewayID,
		SurfaceSessionID:     job.SurfaceSessionID,
		SourceMessageID:      job.SourceMessageID,
		SourceMessagePreview: job.SourceMessagePreview,
		Block:                &result.Block,
		FileChangeSummary:    job.FileChangeSummary,
		FinalTurnSummary:     job.FinalTurnSummary,
	})
	if len(ops) == 0 {
		return
	}
	op := ops[0]
	if op.Kind != feishu.OperationSendCard {
		return
	}
	op.Kind = feishu.OperationUpdateCard
	op.MessageID = anchor.MessageID
	op.ReplyToMessageID = ""

	applyCtx, applyCancel := a.newTimeoutContext(context.Background(), gatewayTimeout)
	err = gateway.Apply(applyCtx, []feishu.Operation{op})
	applyCancel()
	if err != nil {
		if a.observeFeishuPermissionError(job.GatewayID, err) {
			log.Printf("second-chance final patch observed feishu permission gap: gateway=%s surface=%s err=%v", job.GatewayID, job.SurfaceSessionID, err)
			return
		}
		log.Printf(
			"second-chance final patch apply failed: surface=%s thread=%s turn=%s item=%s message=%s err=%v",
			job.SurfaceSessionID,
			job.SentBlock.ThreadID,
			job.SentBlock.TurnID,
			job.SentBlock.ItemID,
			anchor.MessageID,
			err,
		)
		return
	}
}

func secondChanceFinalPreviewTimeout(base time.Duration) time.Duration {
	if base <= 0 {
		return minSecondChanceFinalPreviewTimeout
	}
	timeout := 2 * base
	if timeout < minSecondChanceFinalPreviewTimeout {
		return minSecondChanceFinalPreviewTimeout
	}
	return timeout
}

func sameFinalPatchBlock(left, right render.Block) bool {
	return left.Kind == right.Kind &&
		left.Language == right.Language &&
		left.Final == right.Final &&
		strings.TrimSpace(left.Text) == strings.TrimSpace(right.Text)
}
