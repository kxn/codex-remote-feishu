package feishu

import "strings"

func temporarySessionHeaderSubtitle(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	return "**" + label + "**"
}

func withTemporarySessionCardDocument(doc *cardDocument, label string) *cardDocument {
	if doc == nil {
		return nil
	}
	subtitle := temporarySessionHeaderSubtitle(label)
	if subtitle == "" {
		return doc
	}
	return newCardDocumentWithHeader(
		doc.Title,
		doc.TitleTag,
		subtitle,
		cardTextTagLarkMarkdown,
		doc.ThemeKey,
		doc.Components...,
	)
}

func applyTemporarySessionHeaderToOperation(operation Operation, label string) Operation {
	subtitle := temporarySessionHeaderSubtitle(label)
	if subtitle == "" {
		return operation
	}
	operation.CardSubtitle = subtitle
	operation.CardSubtitleTag = cardTextTagLarkMarkdown
	if operation.card != nil {
		operation.card = withTemporarySessionCardDocument(operation.card, label)
	}
	return operation
}
