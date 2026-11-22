package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamHistoryEntry struct {
	id, body, who string
	at            time.Time
}

func (a *App) queueTeamContextHistory(ctx context.Context, source teamHumanSource, contextID string, page int) (string, error) {
	if page < 0 {
		return "", fmt.Errorf("无效页码")
	}
	var wc work.WorkContext
	var nav work.PrivateNavigation
	var entries []teamHistoryEntry
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		var e error
		wc, e = tx.GetContext(contextID)
		if e != nil {
			return e
		}
		if wc.ScopeID != source.ScopeID || wc.RoleID != source.RoleID || wc.Purpose == work.PurposeControl {
			return workstore.ErrScopeDenied
		}
		nav, e = tx.GetPrivateNavigation(source.ScopeID)
		if e != nil {
			return e
		}
		if wc.Purpose == work.PurposeReview {
			entries, e = teamReviewHistoryEntries(tx, wc)
		} else {
			entries, e = teamDiscussionHistoryEntries(tx, wc)
		}
		return e
	})
	if err != nil {
		return "", err
	}
	// Large conversations use bounded plain-text blocks so the Back action
	// remains available even when the original reply spans multiple cards.
	var blocks []string
	for _, entry := range entries {
		blocks = append(blocks, teamHistoryBlocks(entry.who, entry.body)...)
	}
	if strings.HasPrefix(wc.Workstream, "legacy:") {
		legacy, e := a.teamClaimedLegacyHistory(ctx, source, contextID)
		if e != nil {
			return "", e
		}
		for _, line := range legacy {
			blocks = append(blocks, teamHistoryBlocks("已认领记录", line)...)
		}
	}
	const perPage = 5
	pages := max(1, (len(blocks)+perPage-1)/perPage)
	if page >= pages {
		return "", workstore.ErrConflict
	}
	panel := workui.Panel{Title: "会话历史"}
	if wc.Purpose == work.PurposeReview {
		panel.Title = "只读审查历史"
	}
	if len(blocks) == 0 {
		panel.Paragraphs = append(panel.Paragraphs, "这项工作尚无已保存的讨论记录。")
	} else {
		panel.Paragraphs = append(panel.Paragraphs, blocks[page*perPage:min((page+1)*perPage, len(blocks))]...)
	}
	if pages > 1 {
		panel.Paragraphs = append(panel.Paragraphs, fmt.Sprintf("第 %d / %d 页", page+1, pages))
	}
	addPage := func(label string, target int) {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: label, Action: "history.read", Values: map[string]any{"context_id": contextID, "page": strconv.Itoa(target)}})
	}
	if page > 0 {
		addPage("上一页", page-1)
	}
	if page+1 < pages {
		addPage("下一页", page+1)
	}
	if wc.Purpose != work.PurposeReview {
		panel.Buttons = append(panel.Buttons, workui.Button{Label: "选择这项工作继续", Action: "history.select-context", Values: map[string]any{"context_id": contextID}})
	}
	panel.Buttons = append(panel.Buttons, workui.Button{Label: "返回会话列表", Action: "history.contexts-page", Values: map[string]any{"page": "0"}})
	source.NavigationRevision = nav.Revision
	return a.queueTeamPicker(ctx, source, "private.context-picker", contextID, nav.Revision, panel)
}

func teamReviewHistoryEntries(tx *workstore.Tx, wc work.WorkContext) ([]teamHistoryEntry, error) {
	id, err := tx.LatestReviewPresentation(wc.ID)
	if errors.Is(err, workstore.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	presentation, err := loadTeamReviewPresentation(tx, id)
	if err != nil {
		return nil, err
	}
	content, err := tx.GetContent(wc.ScopeID, presentation.BodyRef)
	if err != nil {
		return nil, err
	}
	label := "审查结果"
	if presentation.Failed {
		label = "审查未完成"
	}
	return []teamHistoryEntry{{id: id, at: content.CreatedAt, who: label, body: string(content.Data)}}, nil
}

func teamDiscussionHistoryEntries(tx *workstore.Tx, wc work.WorkContext) ([]teamHistoryEntry, error) {
	inputs, err := tx.ListInbox(wc.ScopeID)
	if err != nil {
		return nil, err
	}
	entries := []teamHistoryEntry{}
	seen := map[string]bool{}
	for _, input := range inputs {
		if input.RecipientContextID != wc.ID || input.MessageID == "" || seen[input.MessageID] {
			continue
		}
		message, e := tx.GetMessage(input.MessageID)
		if e != nil {
			return nil, e
		}
		if message.ScopeID != wc.ScopeID {
			return nil, workstore.ErrScopeDenied
		}
		if message.Source.Kind != work.ActorPrincipal && message.Source.Kind != work.ActorRole {
			continue
		}
		body, e := tx.GetContent(wc.ScopeID, message.BodyRef)
		if e != nil {
			return nil, e
		}
		who := "助手"
		if message.Source.Kind == work.ActorPrincipal {
			who = "你"
		}
		if input.State == work.InboxCancelled {
			who += "（已取消）"
		}
		text := string(body.Data)
		if message.RecalledEvidenceRef != "" {
			text = "[消息已撤回]"
		} else {
			for _, attachment := range message.Attachments {
				text += "\n附件：" + attachment.FileName
			}
		}
		seen[message.ID] = true
		entries = append(entries, teamHistoryEntry{id: message.ID, at: message.CreatedAt, who: who, body: text})
	}
	replies, err := teamPublishedHistoryEntries(tx, wc)
	if err != nil {
		return nil, err
	}
	entries = append(entries, replies...)
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].at.Equal(entries[j].at) {
			return entries[i].id < entries[j].id
		}
		return entries[i].at.Before(entries[j].at)
	})
	return entries, nil
}

func teamPublishedHistoryEntries(tx *workstore.Tx, wc work.WorkContext) ([]teamHistoryEntry, error) {
	operations, err := tx.ListOperations(wc.ScopeID)
	if err != nil {
		return nil, err
	}
	var entries []teamHistoryEntry
	for _, op := range operations {
		if op.ContextID != wc.ID || op.Kind != "feishu.publish" {
			continue
		}
		projection, e := workservice.ReadMessageProjection(tx, op)
		if e != nil {
			return nil, e
		}
		if projection.SourceRunRef == "" || projection.RoleID != wc.RoleID || projection.ChannelGeneration != 0 {
			continue
		}
		var body string
		if display := projection.PrivateDisplay; display != nil && display.ResultBodyRef != "" {
			stored, e := tx.GetContent(wc.ScopeID, display.ResultBodyRef)
			if e != nil {
				return nil, e
			}
			body = string(stored.Data)
		} else if projection.PrivateDisplay == nil && op.State == work.OperationConfirmed && op.Actor.Kind == work.ActorRole {
			body = projection.Body
		}
		if strings.TrimSpace(body) != "" {
			entries = append(entries, teamHistoryEntry{id: op.ID, at: op.CreatedAt, who: "助手", body: body})
		}
	}
	return entries, nil
}

func teamHistoryBlocks(who, body string) []string {
	const maxRunes = 700
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	text := []rune(body)
	var result []string
	for start := 0; start < len(text); start += maxRunes {
		end := min(start+maxRunes, len(text))
		label := who
		if start > 0 {
			label += "（续）"
		}
		result = append(result, label+"：\n"+string(text[start:end]))
	}
	return result
}
