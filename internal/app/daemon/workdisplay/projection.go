package workdisplay

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type projection struct {
	tx           *workstore.Tx
	display      *workstore.PrivateExecutionDisplay
	event        agentproto.Event
	backend      work.Backend
	now          time.Time
	ordinal      int
	deferPreview bool
}

func (p *projection) enqueue(card json.RawMessage, media *workservice.PrivateMediaPublication, anchor, fence, resultHash string) (string, error) {
	return p.enqueueOutput(card, media, workservice.PrivateDisplayPublication{
		DisplayID: p.display.ID, PreviousOutboxID: p.display.LastOutboxID, AnchorOutboxID: anchor, PatchFenceID: fence,
		ResultHash: resultHash,
	})
}

func (p *projection) enqueueOutput(card json.RawMessage, media *workservice.PrivateMediaPublication, native workservice.PrivateDisplayPublication) (string, error) {
	if len(card) == 0 && media == nil {
		return "", nil
	}
	v := p.display
	id := p.outputID()
	projection := workservice.MessageProjection{
		Card: card, Media: media, PrivateDisplay: &native, SourceRunRef: v.Binding.RunRef,
		RoleID: v.Bot.RoleID, GatewayRef: v.Bot.GatewayRef, AppID: v.Bot.AppID, BotGeneration: v.Bot.IdentityGeneration,
		Tenant: v.Tenant, ChatID: v.ChatID, ReplyToMessageID: v.ReplyToMessageID,
	}
	outboxID, err := queue(p.tx, v.Binding.ScopeID, v.Binding.ContextID, id, projection, p.now)
	if err != nil {
		return "", err
	}
	v.LastOutboxID = outboxID
	return outboxID, nil
}

func queue(tx *workstore.Tx, scopeID, contextID, id string, projection workservice.MessageProjection, now time.Time) (string, error) {
	payload, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	content := work.NewContent(stableID("body", id), scopeID, "application/json", payload, now)
	if err := tx.PutContent(content); err != nil {
		return "", err
	}
	op := work.Operation{
		ID: id, ScopeID: scopeID, ContextID: contextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "private-execution-display"},
		Kind: "feishu.publish", TargetID: projection.ChatID, TargetGeneration: projection.BotGeneration,
		IdempotencyKey: id, RequestRef: content.ID, RequestHash: content.SHA256,
		State: work.OperationPrepared, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := tx.PutOperation(op, 0); err != nil || !created {
		return id, err
	}
	item := work.Outbox{ID: id, ScopeID: scopeID, OperationID: id, Target: projection.ChatID, EventID: id, PayloadRef: content.ID, State: work.OutboxPending, Revision: 1}
	return id, tx.PutOutbox(item, 0)
}

func (p *projection) finalReply(body string) error {
	hash := workservice.PrivateResultHash(body)
	duplicate, err := p.finalAlreadyPublished(hash)
	if err != nil || duplicate {
		return err
	}
	return p.reply("", body, p.finalSummary(), hash)
}

func (p *projection) finalAlreadyPublished(hash string) (bool, error) {
	v := p.display
	ops, err := p.tx.PrivateRunPublications(v.Binding.ScopeID, v.Binding.ContextID, v.Binding.RunRef)
	if err != nil {
		return false, err
	}
	for _, op := range ops {
		projected, e := workservice.ReadMessageProjection(p.tx, op)
		if e != nil {
			return false, e
		}
		if projected.ChatID != v.ChatID || projected.Tenant != v.Tenant || projected.BotGeneration != v.Bot.IdentityGeneration || projected.AppID != v.Bot.AppID || projected.GatewayRef != v.Bot.GatewayRef {
			continue
		}
		if projected.PrivateDisplay == nil && projected.Body != "" && workservice.PrivateResultHash(projected.Body) == hash {
			return true, nil
		}
		if projected.PrivateDisplay != nil && projected.PrivateDisplay.ResultHash == hash {
			return true, nil
		}
	}
	return false, nil
}

func (p *projection) reply(title, body string, summary *control.FinalTurnSummary, resultHash string) error {
	var files *control.FileChangeSummary
	var diff *control.TurnDiffPreview
	originalBody := body
	if resultHash != "" {
		files = p.fileSummary()
		if p.display.PreviewState == "ready" && p.display.PreviewSource == body {
			body, diff = p.display.PreviewBody, p.display.PreviewDiff
		}
	}
	cards := feishu.PrivateExecutionReplyWithPreview(title, body, files, diff, summary)
	for i, card := range cards {
		meta := workservice.PrivateDisplayPublication{DisplayID: p.display.ID, PreviousOutboxID: p.display.LastOutboxID, ResultHash: resultHash}
		if i == 0 && resultHash != "" {
			content := work.NewContent(stableID("final-body", p.display.ID), p.display.Binding.ScopeID, "text/markdown", []byte(originalBody), p.now)
			if err := p.tx.PutContent(content); err != nil {
				return err
			}
			meta.ResultBodyRef = content.ID
		}
		if _, err := p.enqueueOutput(card, nil, meta); err != nil {
			return err
		}
	}
	p.display.HadOutput = true
	return nil
}

func (p *projection) fileSummary() *control.FileChangeSummary {
	if len(p.display.Files) == 0 {
		return nil
	}
	summary := &control.FileChangeSummary{ThreadID: p.event.ThreadID, FileCount: len(p.display.Files)}
	for _, file := range p.display.Files {
		summary.Files = append(summary.Files, file)
		summary.AddedLines += file.AddedLines
		summary.RemovedLines += file.RemovedLines
	}
	sort.Slice(summary.Files, func(i, j int) bool { return summary.Files[i].Path < summary.Files[j].Path })
	return summary
}

func (p *projection) finalSummary() *control.FinalTurnSummary {
	var summary control.FinalTurnSummary
	if !p.display.StartedAt.IsZero() && !p.now.Before(p.display.StartedAt) {
		summary.Elapsed = p.now.Sub(p.display.StartedAt)
	}
	usage := p.event.TokenUsage
	if usage == nil {
		usage = p.display.LatestTokenUsage
	}
	if usage != nil {
		summary.Usage = usageBreakdown(usage.Last)
		if p.backend != work.BackendClaude {
			summary.UsageLabel = "最近一次模型请求"
		}
		summary.ThreadUsage = usageBreakdown(usage.Total)
		summary.ModelContextWindow = usage.ModelContextWindow
		if usage.Last.InputTokens > 0 {
			input := usage.Last.InputTokens
			summary.ContextInputTokens = &input
		}
	}
	return &summary
}

func usageBreakdown(usage agentproto.TokenUsageBreakdown) *control.FinalTurnUsage {
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
		return nil
	}
	return &control.FinalTurnUsage{
		InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens, OutputTokens: usage.OutputTokens,
		ReasoningOutputTokens: usage.ReasoningOutputTokens, TotalTokens: usage.TotalTokens,
	}
}

func (p *projection) compactPendingProgress() (fenceID string, err error) {
	v := p.display
	fenceID = v.ProgressFenceID
	if fenceID == "" || fenceID == v.ProgressAnchorID || fenceID != v.LastOutboxID {
		return fenceID, nil
	}
	item, err := p.tx.GetOutbox(fenceID)
	if err != nil {
		return "", err
	}
	op, err := p.tx.GetOperation(item.OperationID)
	if err != nil {
		return "", err
	}
	old, err := workservice.ReadMessageProjection(p.tx, op)
	if err != nil {
		return "", err
	}
	if old.PrivateDisplay == nil || old.PrivateDisplay.DisplayID != v.ID || old.PrivateDisplay.AnchorOutboxID != v.ProgressAnchorID {
		return "", workstore.ErrScopeDenied
	}
	canceled, err := p.tx.SupersedePrivateDisplayUpdate(fenceID, p.now)
	if err == nil && canceled {
		v.LastOutboxID = old.PrivateDisplay.PreviousOutboxID
		return old.PrivateDisplay.PatchFenceID, nil
	}
	return fenceID, err
}
