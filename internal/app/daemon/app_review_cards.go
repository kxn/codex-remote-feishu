package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const reviewCardTitlePrefix = "审阅中 · "

func (a *App) decorateReviewOperationsLocked(event eventcontract.Event, operations []feishu.Operation) []feishu.Operation {
	if len(operations) == 0 {
		return operations
	}
	if event.Kind == eventcontract.KindBlockCommitted && event.Block != nil && event.Block.Final {
		if isReviewFinal, targetLabel, keepExitButtons := a.reviewFinalBlockDecorationContext(event.SurfaceSessionID, *event.Block); isReviewFinal {
			for i := range operations {
				if operations[i].Kind != feishu.OperationSendCard && operations[i].Kind != feishu.OperationUpdateCard {
					continue
				}
				addReviewCardTitlePrefix(&operations[i], targetLabel)
			}
			if keepExitButtons {
				if primary := firstFinalSendCard(operations); primary != nil {
					appendFooterButtons(primary, reviewExitButtons(a.daemonLifecycleID))
				}
			}
			return operations
		}
		addUncommittedEntry := a.service.CanOfferUncommittedReviewForFinalBlock(event.SurfaceSessionID, *event.Block)
		addedUncommittedEntry := false
		for i := range operations {
			if operations[i].Kind != feishu.OperationSendCard && operations[i].Kind != feishu.OperationUpdateCard {
				continue
			}
			rawBody := strings.TrimSpace(operations[i].FinalSourceBody())
			if rawBody == "" {
				continue
			}
			buttons := make([]map[string]any, 0, 4)
			if addUncommittedEntry && !addedUncommittedEntry {
				buttons = append(buttons, reviewEntryButton(a.daemonLifecycleID))
				addedUncommittedEntry = true
			}
			if commits := a.service.ResolveFinalBlockCommitReviewTargets(event.SurfaceSessionID, *event.Block, rawBody); len(commits) != 0 {
				buttons = append(buttons, reviewCommitButtons(commits, a.daemonLifecycleID)...)
			}
			if len(buttons) == 0 {
				continue
			}
			appendFooterButtons(&operations[i], buttons)
		}
		return operations
	}
	if a.service.ReviewSession(event.SurfaceSessionID) == nil {
		return operations
	}
	targetLabel := ""
	if session := a.service.ReviewSession(event.SurfaceSessionID); session != nil {
		targetLabel = strings.TrimSpace(session.TargetLabel)
	}
	for i := range operations {
		if operations[i].Kind != feishu.OperationSendCard && operations[i].Kind != feishu.OperationUpdateCard {
			continue
		}
		addReviewCardTitlePrefix(&operations[i], targetLabel)
	}
	return operations
}

func (a *App) reviewFinalBlockDecorationContext(surfaceID string, block render.Block) (isReviewFinal bool, targetLabel string, keepExitButtons bool) {
	if session := a.service.ReviewSession(surfaceID); session != nil && strings.TrimSpace(block.ThreadID) == strings.TrimSpace(session.ReviewThreadID) {
		return true, strings.TrimSpace(session.TargetLabel), true
	}
	instanceID := strings.TrimSpace(block.InstanceID)
	if instanceID == "" {
		if surface := a.service.Surface(surfaceID); surface != nil {
			instanceID = strings.TrimSpace(surface.AttachedInstanceID)
		}
	}
	threadID := strings.TrimSpace(block.ThreadID)
	if instanceID == "" || threadID == "" {
		return false, "", false
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return false, "", false
	}
	thread := inst.Threads[threadID]
	if thread == nil || thread.Source == nil || !thread.Source.IsReview() {
		return false, "", false
	}
	return true, "", false
}

func addReviewCardTitlePrefix(operation *feishu.Operation, targetLabel string) {
	if operation == nil {
		return
	}
	title := strings.TrimSpace(operation.CardTitle)
	if title == "" {
		title = "审阅中"
	}
	prefix := reviewCardTitlePrefix
	if targetLabel = strings.TrimSpace(targetLabel); targetLabel != "" {
		prefix += targetLabel + " · "
	}
	if !strings.HasPrefix(title, prefix) {
		if strings.HasPrefix(title, reviewCardTitlePrefix) {
			title = strings.TrimPrefix(title, reviewCardTitlePrefix)
		}
		title = prefix + title
	}
	operation.CardTitle = title
	feishu.InvalidateOperationCard(operation)
}

func appendFooterButtons(operation *feishu.Operation, buttons []map[string]any) {
	if operation == nil || len(buttons) == 0 {
		return
	}
	elements := cloneCardElements(operation.CardElements)
	if len(elements) != 0 {
		elements = append(elements, map[string]any{"tag": "hr"})
	}
	elements = append(elements, cardButtonGroupElement(buttons))
	operation.CardElements = elements
	feishu.InvalidateOperationCard(operation)
}

func reviewEntryButton(daemonLifecycleID string) map[string]any {
	return localCurrentCardActionButton(
		"审阅待提交内容",
		"primary",
		daemonLifecycleID,
		control.ActionReviewStart,
		"",
	)
}

func reviewCommitButtons(commits []gitmeta.CommitSummary, daemonLifecycleID string) []map[string]any {
	buttons := make([]map[string]any, 0, len(commits))
	for _, commit := range commits {
		commit = commit.Normalized()
		if commit.SHA == "" {
			continue
		}
		buttons = append(buttons, localCurrentCardActionButton(
			fmt.Sprintf("审阅 %s", reviewCommitButtonSHA(firstNonEmpty(commit.ShortSHA, commit.SHA))),
			"default",
			daemonLifecycleID,
			control.ActionReviewCommand,
			"commit "+strings.TrimSpace(commit.SHA),
		))
	}
	return buttons
}

func reviewCommitButtonSHA(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) >= 7 {
		return value[:7]
	}
	return value
}

func reviewExitButtons(daemonLifecycleID string) []map[string]any {
	return []map[string]any{
		localCurrentCardActionButton(
			"放弃审阅",
			"default",
			daemonLifecycleID,
			control.ActionReviewDiscard,
			"",
		),
		localCurrentCardActionButton(
			"按审阅意见继续修改",
			"primary",
			daemonLifecycleID,
			control.ActionReviewApply,
			"",
		),
	}
}

func stampActionPayload(value map[string]any, daemonLifecycleID string) map[string]any {
	return frontstagecontract.ActionPayloadWithLifecycle(value, daemonLifecycleID)
}

func localCurrentCardActionButton(label, buttonType, daemonLifecycleID string, actionKind control.ActionKind, actionArg string) map[string]any {
	return cardCallbackButton(
		label,
		buttonType,
		stampActionPayload(control.FeishuLocalCardActionPayload(actionKind, actionArg), daemonLifecycleID),
	)
}

func cloneCardElements(elements []map[string]any) []map[string]any {
	if len(elements) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(elements))
	for _, element := range elements {
		out = append(out, cloneCardMap(element))
	}
	return out
}

func cloneCardMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, raw := range value {
		out[key] = cloneCardValue(raw)
	}
	return out
}

func cloneCardValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneCardMap(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneCardMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneCardValue(item))
		}
		return out
	default:
		return typed
	}
}

func cardPlainText(content string) map[string]any {
	return map[string]any{
		"tag":     "plain_text",
		"content": strings.TrimSpace(content),
	}
}

func cardCallbackButton(label, buttonType string, value map[string]any) map[string]any {
	if strings.TrimSpace(buttonType) == "" {
		buttonType = "default"
	}
	return map[string]any{
		"tag":  "button",
		"type": buttonType,
		"text": cardPlainText(label),
		"behaviors": []map[string]any{{
			"type":  "callback",
			"value": cloneCardMap(value),
		}},
	}
}

func cardButtonGroupElement(buttons []map[string]any) map[string]any {
	filtered := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		if len(button) == 0 {
			continue
		}
		filtered = append(filtered, cloneCardMap(button))
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		columns := make([]map[string]any, 0, len(filtered))
		for _, button := range filtered {
			columns = append(columns, map[string]any{
				"tag":            "column",
				"width":          "auto",
				"vertical_align": "top",
				"elements":       []map[string]any{button},
			})
		}
		return map[string]any{
			"tag":                "column_set",
			"flex_mode":          "flow",
			"horizontal_spacing": "small",
			"columns":            columns,
		}
	}
}

func (a *App) reviewSessionState(surfaceID string) *state.ReviewSessionRecord {
	return a.service.ReviewSession(surfaceID)
}
