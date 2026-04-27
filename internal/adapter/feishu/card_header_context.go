package feishu

import "strings"

func detourHeaderSubtitle(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	return "**" + label + "**"
}

func withDetourCardDocument(doc *cardDocument, label string) *cardDocument {
	if doc == nil {
		return nil
	}
	subtitle := detourHeaderSubtitle(label)
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

func applyDetourHeaderToOperation(operation Operation, label string) Operation {
	subtitle := detourHeaderSubtitle(label)
	if subtitle == "" {
		return operation
	}
	operation.CardSubtitle = subtitle
	operation.CardSubtitleTag = cardTextTagLarkMarkdown
	if operation.card != nil {
		operation.card = withDetourCardDocument(operation.card, label)
	}
	return operation
}
