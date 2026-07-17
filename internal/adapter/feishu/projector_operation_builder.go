package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type eventCardOperationSpec struct {
	Title                 string
	TitleTag              string
	Subtitle              string
	SubtitleTag           string
	Body                  string
	DocumentBody          string
	UseDocumentBody       bool
	ThemeKey              string
	Elements              []map[string]any
	MessageID             string
	UpdateMulti           bool
	TemporarySessionLabel string
	ProgressCardStartSeq  int
	ProgressCardEndSeq    int
	ApplyReplyLane        bool
}

func newEventCardOperation(chatID string, event eventcontract.Event, spec eventCardOperationSpec) Operation {
	title := strings.TrimSpace(spec.Title)
	theme := strings.TrimSpace(spec.ThemeKey)
	body := spec.Body
	documentBody := spec.DocumentBody
	if !spec.UseDocumentBody {
		documentBody = body
	}
	elements := spec.Elements
	operation := Operation{
		Kind:                 OperationSendCard,
		GatewayID:            event.GatewayID,
		SurfaceSessionID:     event.SurfaceSessionID,
		ChatID:               chatID,
		MessageID:            strings.TrimSpace(spec.MessageID),
		CardTitle:            title,
		CardTitleTag:         strings.TrimSpace(spec.TitleTag),
		CardSubtitle:         strings.TrimSpace(spec.Subtitle),
		CardSubtitleTag:      strings.TrimSpace(spec.SubtitleTag),
		CardBody:             body,
		CardThemeKey:         theme,
		CardElements:         elements,
		CardUpdateMulti:      spec.UpdateMulti,
		ProgressCardStartSeq: spec.ProgressCardStartSeq,
		ProgressCardEndSeq:   spec.ProgressCardEndSeq,
		cardEnvelope:         cardEnvelopeV2,
		card: rawCardDocumentWithHeader(
			title,
			firstNonEmpty(strings.TrimSpace(spec.TitleTag), cardTextTagPlainText),
			spec.Subtitle,
			firstNonEmpty(strings.TrimSpace(spec.SubtitleTag), cardTextTagLarkMarkdown),
			documentBody,
			theme,
			elements,
		),
	}
	if operation.MessageID != "" {
		operation.Kind = OperationUpdateCard
		operation.ReplyToMessageID = ""
	} else if spec.ApplyReplyLane {
		operation = applyReplyLaneToNewOperation(event, operation)
	}
	return applyTemporarySessionHeaderToOperation(operation, spec.TemporarySessionLabel)
}
