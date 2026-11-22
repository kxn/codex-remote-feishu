package feishu

import (
	"encoding/json"
	"strings"

	feishuprojector "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/projector"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func PrivateExecutionProgress(progress control.ExecCommandProgress, plan *control.PlanUpdate, title string) (card json.RawMessage, startSeq int, rollover bool) {
	ops := (&Projector{}).projectExecCommandProgress("", eventcontract.Event{}, progress)
	if len(ops) == 0 {
		return nil, 0, false
	}
	op := ops[0]
	if title == "" {
		title = op.CardTitle
	}
	baseElements := op.CardElements
	elements := baseElements
	if plan != nil {
		elements = append(append([]map[string]any(nil), baseElements...), feishuprojector.PlanUpdateElements(*plan)...)
	}
	op.card = rawCardDocumentWithHeader(title, cardTextTagPlainText, op.CardSubtitle, op.CardSubtitleTag, "", op.CardThemeKey, elements)
	card = executionCardJSON(op)
	if len(card) == 0 && plan != nil {
		op.card = rawCardDocumentWithHeader(title, cardTextTagPlainText, op.CardSubtitle, op.CardSubtitleTag, "", op.CardThemeKey, baseElements)
		card = executionCardJSON(op)
	}
	return card, op.ProgressCardStartSeq, op.Kind != OperationUpdateCard && len(progress.Segments) != 0
}

func PrivateExecutionReply(title, markdown string, files *control.FileChangeSummary, summary *control.FinalTurnSummary) []json.RawMessage {
	elements := (&Projector{}).finalBlockExtraElements(files, nil, summary)
	ops := projectFinalReplyCards("", "", "", "", title, "", markdown, elements)
	cards := make([]json.RawMessage, 0, len(ops))
	for _, op := range ops {
		cards = append(cards, executionCardJSON(op))
	}
	return cards
}

func PrivateExecutionNotice(title, message string) json.RawMessage {
	return executionCardJSON(Operation{
		CardTitle:    title,
		CardThemeKey: cardThemeProgress,
		card: newCardDocumentWithHeader(title, cardTextTagPlainText, "", "", cardThemeProgress,
			cardRawComponent{data: map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": message}}}),
	})
}

func PrivatePublicSummary(text string) string {
	return renderFinalCardMarkdown(text)
}

func executionCardJSON(op Operation) json.RawMessage {
	card := renderOperationCard(op, op.effectiveCardEnvelope())
	if !executionCardPayloadFits(card) {
		return nil
	}
	raw, err := json.Marshal(card)
	if err != nil || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	return raw
}

func executionCardPayloadFits(payload map[string]any) bool {
	raw, err := json.Marshal(payload)
	return payload != nil && err == nil && len(raw) <= 28*1024 && feishuInteractiveMessageTransportFits(payload)
}
