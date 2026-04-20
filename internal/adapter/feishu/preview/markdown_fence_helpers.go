package preview

import "strings"

type finalCardFenceSegment struct {
	fenced bool
	text   string
}

func splitFinalCardFenceSegments(text string) []finalCardFenceSegment {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 {
		return []finalCardFenceSegment{{text: text}}
	}
	segments := make([]finalCardFenceSegment, 0, len(lines))
	var current strings.Builder
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	flush := func(fenced bool) {
		if current.Len() == 0 {
			return
		}
		segments = append(segments, finalCardFenceSegment{
			fenced: fenced,
			text:   current.String(),
		})
		current.Reset()
	}
	for _, line := range lines {
		char, count, ok := finalCardFenceMarker(line)
		switch {
		case !inFence && ok:
			flush(false)
			current.WriteString(line)
			inFence = true
			fenceChar = char
			fenceLen = count
		case inFence:
			current.WriteString(line)
			if ok && char == fenceChar && count >= fenceLen {
				flush(true)
				inFence = false
				fenceChar = 0
				fenceLen = 0
			}
		default:
			current.WriteString(line)
		}
	}
	flush(inFence)
	return segments
}

func finalCardFenceMarker(line string) (byte, int, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 3 {
		return 0, 0, false
	}
	switch trimmed[0] {
	case '`', '~':
		char := trimmed[0]
		count := 1
		for count < len(trimmed) && trimmed[count] == char {
			count++
		}
		if count >= 3 {
			return char, count, true
		}
	}
	return 0, 0, false
}

func consecutiveByteRun(text string, start int, target byte) int {
	count := 0
	for start+count < len(text) && text[start+count] == target {
		count++
	}
	return count
}

func closingBacktickRun(text string, start, run int) int {
	for i := start; i < len(text); i++ {
		if text[i] != '`' {
			continue
		}
		if consecutiveByteRun(text, i, '`') == run {
			return i
		}
	}
	return -1
}

func parseMarkdownLinkAt(text string, start int) (end int, label, target string, ok bool) {
	if start < 0 || start >= len(text) || text[start] != '[' {
		return 0, "", "", false
	}
	labelEnd := strings.IndexByte(text[start+1:], ']')
	if labelEnd < 0 {
		return 0, "", "", false
	}
	labelEnd += start + 1
	if labelEnd+1 >= len(text) || text[labelEnd+1] != '(' {
		return 0, "", "", false
	}
	targetEnd := strings.IndexByte(text[labelEnd+2:], ')')
	if targetEnd < 0 {
		return 0, "", "", false
	}
	targetEnd += labelEnd + 2
	return targetEnd + 1, text[start+1 : labelEnd], text[labelEnd+2 : targetEnd], true
}
