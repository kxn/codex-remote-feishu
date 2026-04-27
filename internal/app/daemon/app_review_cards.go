package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const reviewCardTitlePrefix = "审阅中 · "

func (a *App) decorateReviewOperationsLocked(event eventcontract.Event, operations []feishu.Operation) []feishu.Operation {
	if len(operations) == 0 {
		return operations
	}
	if event.Kind == eventcontract.KindBlockCommitted && event.Block != nil && event.Block.Final {
		if session := a.service.ReviewSession(event.SurfaceSessionID); session != nil && strings.TrimSpace(event.Block.ThreadID) == strings.TrimSpace(session.ReviewThreadID) {
			for i := range operations {
				if operations[i].Kind != feishu.OperationSendCard && operations[i].Kind != feishu.OperationUpdateCard {
					continue
				}
				addReviewCardTitlePrefix(&operations[i])
			}
			if primary := firstFinalSendCard(operations); primary != nil {
				appendFooterButtons(primary, reviewExitButtons(a.daemonLifecycleID))
			}
			return operations
		}
		if !finalCardHasFileChanges(event) {
			return operations
		}
		for i := range operations {
			if operations[i].Kind != feishu.OperationSendCard && operations[i].Kind != feishu.OperationUpdateCard {
				continue
			}
			if strings.TrimSpace(operations[i].FinalSourceBody()) == "" {
				continue
			}
			appendFooterButtons(&operations[i], []map[string]any{reviewEntryButton(a.daemonLifecycleID)})
			break
		}
		return operations
	}
	if a.service.ReviewSession(event.SurfaceSessionID) == nil {
		return operations
	}
	for i := range operations {
		if operations[i].Kind != feishu.OperationSendCard && operations[i].Kind != feishu.OperationUpdateCard {
			continue
		}
		addReviewCardTitlePrefix(&operations[i])
	}
	return operations
}

func finalCardHasFileChanges(event eventcontract.Event) bool {
	summary := event.FileChangeSummary
	return summary != nil && (summary.FileCount > 0 || len(summary.Files) > 0)
}

func addReviewCardTitlePrefix(operation *feishu.Operation) {
	if operation == nil {
		return
	}
	title := strings.TrimSpace(operation.CardTitle)
	if title == "" {
		title = "审阅中"
	}
	if !strings.HasPrefix(title, reviewCardTitlePrefix) {
		title = reviewCardTitlePrefix + title
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
	return cardCallbackButton(
		"进入审阅",
		"primary",
		stampActionPayload(frontstagecontract.ActionPayloadPageAction(string(control.ActionReviewStart), ""), daemonLifecycleID),
	)
}

func reviewExitButtons(daemonLifecycleID string) []map[string]any {
	return []map[string]any{
		cardCallbackButton(
			"放弃审阅",
			"default",
			stampActionPayload(frontstagecontract.ActionPayloadPageAction(string(control.ActionReviewDiscard), ""), daemonLifecycleID),
		),
		cardCallbackButton(
			"按审阅意见继续修改",
			"primary",
			stampActionPayload(frontstagecontract.ActionPayloadPageAction(string(control.ActionReviewApply), ""), daemonLifecycleID),
		),
	}
}

func stampActionPayload(value map[string]any, daemonLifecycleID string) map[string]any {
	return frontstagecontract.ActionPayloadWithLifecycle(value, daemonLifecycleID)
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
