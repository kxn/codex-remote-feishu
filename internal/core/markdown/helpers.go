// Package markdown provides shared markdown fence and link parsing helpers
// used by the feishu card renderer and the feishu preview rewriter.
package markdown

import "strings"

// FenceSegment represents a contiguous run of text that is either inside a
// markdown fenced code block or outside of one.
type FenceSegment struct {
	Fenced bool
	Text   string
}

// SplitFenceSegments splits text into alternating fenced / non-fenced segments
// by scanning for markdown code fence markers (``` or ~~~).
func SplitFenceSegments(text string) []FenceSegment {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 {
		return []FenceSegment{{Text: text}}
	}
	segments := make([]FenceSegment, 0, len(lines))
	var current strings.Builder
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	flush := func(fenced bool) {
		if current.Len() == 0 {
			return
		}
		segments = append(segments, FenceSegment{
			Fenced: fenced,
			Text:   current.String(),
		})
		current.Reset()
	}
	for _, line := range lines {
		char, count, ok := FenceMarker(line)
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

// FenceMarker checks whether line is a markdown code fence opener. Returns the
// fence character (backtick or tilde), the run length, and true if it is a
// valid fence marker (>= 3 characters).
func FenceMarker(line string) (byte, int, bool) {
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

// ConsecutiveByteRun counts how many consecutive bytes starting at position
// start in text equal to target.
func ConsecutiveByteRun(text string, start int, target byte) int {
	count := 0
	for start+count < len(text) && text[start+count] == target {
		count++
	}
	return count
}

// ClosingBacktickRun finds the position of a closing backtick run of exactly
// run length, starting from position start. Returns -1 if not found.
func ClosingBacktickRun(text string, start, run int) int {
	for i := start; i < len(text); i++ {
		if text[i] != '`' {
			continue
		}
		if ConsecutiveByteRun(text, i, '`') == run {
			return i
		}
	}
	return -1
}

// ParseMarkdownLinkAt parses a markdown inline link starting at position start
// (which must be '['). Returns the end position (exclusive), the link label,
// the raw target, and true on success. Handles nested parentheses and
// backslash escapes inside the target.
func ParseMarkdownLinkAt(text string, start int) (end int, label, target string, ok bool) {
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
	targetStart := labelEnd + 2
	depth := 0
	for i := targetStart; i < len(text); i++ {
		switch text[i] {
		case '\\':
			if i+1 < len(text) {
				i++
			}
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i + 1, text[start+1 : labelEnd], text[targetStart:i], true
			}
			depth--
		}
	}
	return 0, "", "", false
}
