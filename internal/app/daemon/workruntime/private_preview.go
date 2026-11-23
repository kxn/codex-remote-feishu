package workruntime

import (
	"context"
	"errors"
	"log"
	"strings"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workdisplay"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

// RunPrivatePreviews schedules independent accepted terminal displays without
// holding the journal lane or a store transaction across preview I/O.
func (c *Coordinator) RunPrivatePreviews(ctx context.Context, limit int) error {
	var pending []string
	if err := c.store.Within(ctx, func(tx *workstore.Tx) error { var e error; pending, e = tx.ListPendingPrivatePreviews(limit); return e }); err != nil {
		return err
	}
	for _, id := range pending {
		c.previewMu.Lock()
		if c.previewActive == nil {
			c.previewActive = make(map[string]bool)
		}
		if c.previewActive[id] || len(c.previewActive) >= 16 {
			c.previewMu.Unlock()
			continue
		}
		c.previewActive[id] = true
		c.previewWorkers.Add(1)
		c.previewMu.Unlock()
		go func(id string) {
			defer c.previewWorkers.Done()
			defer func() { c.previewMu.Lock(); delete(c.previewActive, id); c.previewMu.Unlock() }()
			if err := c.preparePrivateFinalPreview(ctx, id); err != nil && ctx.Err() == nil {
				log.Printf("private final preview: %v", err)
			}
		}(id)
	}
	return nil
}

// WaitPrivatePreviews is called after the scheduling lane has stopped.
func (c *Coordinator) WaitPrivatePreviews() { c.previewWorkers.Wait() }

func (c *Coordinator) preparePrivateFinalPreview(ctx context.Context, displayID string) error {
	var scope string
	if err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		v, err := tx.GetPrivateExecutionDisplay(displayID)
		if err != nil {
			return err
		}
		scope = v.Binding.ScopeID
		return nil
	}); err != nil {
		return err
	}
	if c.options.PersonalSourceActivity != nil {
		end, err := c.options.PersonalSourceActivity(ctx, scope)
		if errors.Is(err, workstore.ErrPersonalMigrationFenced) {
			return nil
		}
		if err != nil {
			return err
		}
		defer end()
	}
	var req *previewpkg.FinalBlockPreviewRequest
	var sourceText string
	var bot work.RoleBotBinding
	deferredForMigration := false
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		v, err := tx.GetPrivateExecutionDisplay(displayID)
		if err != nil {
			return err
		}
		if v.PreviewTerminal == nil || (v.PreviewState != "queued" && v.PreviewState != "started") {
			return nil
		}
		if e := tx.AssertPersonalMigrationOpen(v.Binding.ScopeID); errors.Is(e, workstore.ErrPersonalMigrationFenced) {
			deferredForMigration = true
			return nil
		} else if e != nil {
			return e
		}
		old := v.Revision
		if v.PreviewState == "started" {
			// An interrupted preparation may already have uploaded; never retry it.
			v.PreviewState = "skipped"
			v.Revision++
			if err := tx.PutPrivateExecutionDisplay(v, old); err != nil {
				return err
			}
			return nil
		}
		if c.options.PreviewPrivateFinal == nil {
			v.PreviewState = "skipped"
			v.Revision++
			return tx.PutPrivateExecutionDisplay(v, old)
		}
		event := v.PreviewTerminal
		turn, err := tx.GetNativeTurn(v.Incarnation, v.SessionID, v.TurnID)
		if err != nil || event.Kind != agentproto.EventTurnCompleted || event.Journal == nil || turn.Binding != v.Binding || turn.TerminalSequence != event.Journal.Sequence || v.Pending != v.PreviewSource {
			v.PreviewState = "skipped"
			v.Revision++
			return tx.PutPrivateExecutionDisplay(v, old)
		}
		duplicate, err := workdisplay.EquivalentFinalAlreadyPublished(tx, &v)
		if err != nil {
			return err
		}
		if duplicate {
			v.PreviewState = "skipped"
			v.Revision++
			return tx.PutPrivateExecutionDisplay(v, old)
		}
		src, ok, err := workdisplay.ResolvePreviewSource(tx, v, c.options.HostID)
		if err != nil || !ok {
			v.PreviewState = "skipped"
			v.Revision++
			return tx.PutPrivateExecutionDisplay(v, old)
		}
		sourceText, bot = v.PreviewSource, v.Bot
		carrier, err := tx.GetCarrier(v.Incarnation)
		if err != nil {
			return err
		}
		surface := (feishuidentity.SurfaceRef{Platform: feishuidentity.PlatformFeishu, GatewayID: bot.GatewayRef, ScopeKind: feishuidentity.ScopeKindUser, ScopeID: src.ActorOpenID}).SurfaceID()
		block := render.Block{ID: v.PendingItem, SurfaceSessionID: surface, InstanceID: carrier.OriginInstanceID, ThreadID: v.SessionID, TurnID: v.TurnID, ItemID: v.PendingItem, Kind: render.BlockAssistantMarkdown, Text: sourceText, Final: true}
		req = &previewpkg.FinalBlockPreviewRequest{GatewayID: bot.GatewayRef, SurfaceSessionID: surface, ChatID: v.ChatID, ActorUserID: src.ActorOpenID, WorkspaceRoot: src.WorkspaceRoot, ThreadCWD: src.ThreadCWD, TurnDiffSnapshot: v.TurnDiff, Block: block, PrivateWorkspaceOnly: true}
		v.PreviewState = "started"
		v.Revision++
		return tx.PutPrivateExecutionDisplay(v, old)
	})
	if err != nil {
		return err
	}
	if deferredForMigration {
		return nil
	}
	if req != nil {
		result, previewErr := c.options.PreviewPrivateFinal(ctx, bot, *req)
		if previewErr != nil {
			log.Printf("private final preview incomplete for display %s: %v", displayID, previewErr)
		}
		// The previewer can successfully rewrite one file even when a later
		// upload fails. Keep confirmed links and preserve unmatched source text.
		body, diff := sourceText, result.TurnDiffPreview
		if strings.TrimSpace(result.Block.Text) != "" {
			body = result.Block.Text
		}
		if err := c.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			v, e := tx.GetPrivateExecutionDisplay(displayID)
			if e != nil {
				return e
			}
			if v.PreviewState != "started" || v.PreviewSource != sourceText {
				return nil
			}
			old := v.Revision
			v.PreviewState, v.PreviewBody, v.PreviewDiff = "ready", body, diff
			v.Revision++
			return tx.PutPrivateExecutionDisplay(v, old)
		}); err != nil {
			return err
		}
	}
	var attemptID string
	err = c.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
		var e error
		attemptID, e = workdisplay.FinishPreparedPreview(tx, displayID)
		return e
	})
	if err == nil && attemptID != "" && c.options.EnqueueReconcile != nil {
		c.options.EnqueueReconcile(attemptID)
	}
	return err
}
