package feishu

import (
	"strings"
	"unicode/utf8"
)

type finalReplyChunk struct {
	title        string
	subtitle     string
	sourceBody   string
	renderedBody string
	elements     []map[string]any
}

func splitFinalReplyBodies(rawBody, primaryTitle, primarySubtitle string, primaryElements []map[string]any) []finalReplyChunk {
	rawBody = normalizeFinalCardSource(rawBody)
	primary := newFinalReplyChunk(primaryTitle, primarySubtitle, rawBody, primaryElements)
	if finalReplyChunkFits(primary) {
		return []finalReplyChunk{primary}
	}
	remaining := []string{rawBody}
	chunks := make([]finalReplyChunk, 0, 4)
	first := true
	for len(remaining) > 0 {
		title := primaryTitle
		subtitle := primarySubtitle
		elements := primaryElements
		if !first {
			title = "✅ 最后答复（续）"
			subtitle = ""
			elements = nil
		}
		sourceBody, rest := consumeFinalReplyChunk(remaining, title, subtitle, elements)
		if sourceBody == "" {
			break
		}
		chunks = append(chunks, newFinalReplyChunk(title, subtitle, sourceBody, elements))
		remaining = rest
		first = false
	}
	if len(chunks) == 0 {
		return []finalReplyChunk{primary}
	}
	return chunks
}

func newFinalReplyChunk(title, subtitle, rawBody string, elements []map[string]any) finalReplyChunk {
	return finalReplyChunk{
		title:        strings.TrimSpace(title),
		subtitle:     strings.TrimSpace(subtitle),
		sourceBody:   rawBody,
		renderedBody: renderFinalCardMarkdown(rawBody),
		elements:     elements,
	}
}

func consumeFinalReplyChunk(units []string, title, subtitle string, elements []map[string]any) (string, []string) {
	queue := append([]string(nil), units...)
	for len(queue) > 0 {
		chunk, rest, ok := packFinalReplyUnits(queue, title, subtitle, elements)
		if ok {
			return chunk, rest
		}
		exploded := explodeFinalReplyUnit(queue[0])
		if len(exploded) == 1 && exploded[0] == queue[0] {
			prefix, suffix := splitFinalReplyUnitRunes(queue[0])
			if prefix == "" {
				return "", nil
			}
			queue = append([]string{prefix, suffix}, queue[1:]...)
			continue
		}
		queue = append(exploded, queue[1:]...)
	}
	return "", nil
}

func packFinalReplyUnits(units []string, title, subtitle string, elements []map[string]any) (string, []string, bool) {
	var body strings.Builder
	for i, unit := range units {
		candidate := body.String() + unit
		if finalReplyChunkFits(newFinalReplyChunk(title, subtitle, candidate, elements)) {
			body.WriteString(unit)
			continue
		}
		if body.Len() == 0 {
			return "", nil, false
		}
		return body.String(), append([]string(nil), units[i:]...), true
	}
	return body.String(), nil, true
}

func finalReplyChunkFits(chunk finalReplyChunk) bool {
	op := Operation{
		Kind:            OperationSendCard,
		CardTitle:       chunk.title,
		CardSubtitle:    chunk.subtitle,
		CardSubtitleTag: cardTextTagLarkMarkdown,
		CardBody:        chunk.renderedBody,
		CardThemeKey:    cardThemeFinal,
		CardElements:    chunk.elements,
		cardEnvelope:    cardEnvelopeV2,
		card:            finalReplyCardDocument(chunk.title, chunk.subtitle, chunk.renderedBody, cardThemeFinal, chunk.elements),
	}
	payload := renderOperationCard(op, op.effectiveCardEnvelope())
	return feishuInteractiveMessageTransportFits(payload)
}

func explodeFinalReplyUnit(unit string) []string {
	if unit == "" {
		return []string{unit}
	}
	if parts := explodeFencedFinalReplyUnit(unit); len(parts) > 1 {
		return parts
	}
	if parts := splitPreservingDoubleNewlines(unit); len(parts) > 1 {
		return parts
	}
	if parts := splitPreservingLines(unit); len(parts) > 1 {
		return parts
	}
	prefix, suffix := splitFinalReplyUnitRunes(unit)
	if prefix == "" || suffix == "" {
		return []string{unit}
	}
	return []string{prefix, suffix}
}

func explodeFencedFinalReplyUnit(unit string) []string {
	segments := splitFinalCardFenceSegments(unit)
	if len(segments) != 1 || !segments[0].fenced || segments[0].text != unit {
		return nil
	}
	lines := strings.SplitAfter(unit, "\n")
	if len(lines) < 3 {
		return nil
	}
	open := lines[0]
	closeLine := lines[len(lines)-1]
	openChar, openCount, openOK := finalCardFenceMarker(open)
	closeChar, closeCount, closeOK := finalCardFenceMarker(closeLine)
	if !openOK || !closeOK || openChar != closeChar || closeCount < openCount {
		return nil
	}
	inner := lines[1 : len(lines)-1]
	if len(inner) >= 2 {
		mid := len(inner) / 2
		return []string{
			open + strings.Join(inner[:mid], "") + closeLine,
			open + strings.Join(inner[mid:], "") + closeLine,
		}
	}
	prefix, suffix := splitFinalReplyUnitRunes(inner[0])
	if prefix == "" || suffix == "" {
		return nil
	}
	return []string{
		open + prefix + closeLine,
		open + suffix + closeLine,
	}
}

func splitPreservingDoubleNewlines(text string) []string {
	if !strings.Contains(text, "\n\n") {
		return nil
	}
	parts := make([]string, 0, 8)
	start := 0
	for start < len(text) {
		idx := strings.Index(text[start:], "\n\n")
		if idx < 0 {
			parts = append(parts, text[start:])
			break
		}
		end := start + idx + 2
		parts = append(parts, text[start:end])
		start = end
	}
	if len(parts) <= 1 {
		return nil
	}
	return parts
}

func splitPreservingLines(text string) []string {
	if !strings.Contains(text, "\n") {
		return nil
	}
	parts := strings.SplitAfter(text, "\n")
	if len(parts) <= 1 {
		return nil
	}
	return parts
}

func splitFinalReplyUnitRunes(text string) (string, string) {
	if utf8.RuneCountInString(text) < 2 {
		return "", ""
	}
	target := utf8.RuneCountInString(text) / 2
	if target <= 0 {
		return "", ""
	}
	index := 0
	count := 0
	for index < len(text) && count < target {
		_, size := utf8.DecodeRuneInString(text[index:])
		index += size
		count++
	}
	if index <= 0 || index >= len(text) {
		return "", ""
	}
	return text[:index], text[index:]
}
