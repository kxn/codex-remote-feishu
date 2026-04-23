package preview

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

type neutralizedLocalMarkdownRewrite struct {
	text    string
	errs    []string
	end     int
	changed bool
}

func parseNeutralizedLocalMarkdownLinkPrefix(prefix string) (head, label string, ok bool, preserve bool) {
	prefixEnd := len(prefix)
	for start := 0; start < prefixEnd; {
		end, _, display, matched := parseStandalonePreviewReferenceAt(prefix, start)
		if matched && end == prefixEnd {
			head = prefix[:start]
			label = display
			if neutralizedLocalMarkdownLinkPrefixNeedsPreserve(head) {
				return "", "", false, true
			}
			return head, label, true, false
		}
		_, size := utf8.DecodeRuneInString(prefix[start:])
		start += size
	}

	labelStart := 0
	for cursor := prefixEnd; cursor > 0; {
		r, size := utf8.DecodeLastRuneInString(prefix[:cursor])
		if unicode.IsSpace(r) || strings.ContainsRune("([{\"'<,.;:!?，。；：！？、（【《“‘", r) {
			labelStart = cursor
			break
		}
		cursor -= size
	}
	label = strings.TrimSpace(prefix[labelStart:])
	if label == "" || strings.ContainsAny(label, "`[]()") || strings.ContainsRune(label, '\n') {
		return "", "", false, false
	}
	head = prefix[:labelStart]
	if neutralizedLocalMarkdownLinkPrefixNeedsPreserve(head) {
		return "", "", false, true
	}
	return head, label, true, false
}

func neutralizedLocalMarkdownLinkPrefixNeedsPreserve(head string) bool {
	token := neutralizedLocalMarkdownLinkPrefixTailToken(head)
	return looksLikeNeutralizedURLSchemeTail(token)
}

func neutralizedLocalMarkdownLinkPrefixTailToken(head string) string {
	head = strings.TrimRightFunc(head, unicode.IsSpace)
	if head == "" {
		return ""
	}
	start := len(head)
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(head[:start])
		if unicode.IsSpace(r) || strings.ContainsRune("([{\"'<,.;!?，。；！？、（【《“‘", r) {
			break
		}
		start -= size
	}
	return head[start:]
}

func looksLikeNeutralizedURLSchemeTail(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}

	base := ""
	switch {
	case strings.HasSuffix(token, "://"):
		base = token[:len(token)-3]
	case strings.HasSuffix(token, ":/"):
		base = token[:len(token)-2]
	case strings.HasSuffix(token, ":"):
		base = token[:len(token)-1]
	default:
		return false
	}
	if len(base) < 2 {
		return false
	}
	for i, r := range base {
		switch {
		case i == 0 && ('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z'):
		case i > 0 && ('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

func (p *DriveMarkdownPreviewer) tryRewriteNeutralizedLocalMarkdownLink(
	ctx context.Context,
	req FinalBlockPreviewRequest,
	principals []previewPrincipal,
	runtime *previewRewriteRuntime,
	scopeKey string,
	rewrittenTargets map[string]string,
	text string,
	last int,
	index int,
	baseOffset int,
) (neutralizedLocalMarkdownRewrite, bool) {
	if p == nil || index < 2 || text[index-2:index] != " (" {
		return neutralizedLocalMarkdownRewrite{}, false
	}
	run := consecutiveByteRun(text, index, '`')
	if run == 0 {
		return neutralizedLocalMarkdownRewrite{}, false
	}
	close := closingBacktickRun(text, index+run, run)
	if close < 0 || close+run >= len(text) || text[close+run] != ')' {
		return neutralizedLocalMarkdownRewrite{}, false
	}
	rawTarget := text[index+run : close]
	trimStart, trimEnd := trimMarkdownInlineSpaceBounds(rawTarget)
	if trimStart >= trimEnd {
		return neutralizedLocalMarkdownRewrite{}, false
	}
	rawTarget = rawTarget[trimStart:trimEnd]
	head, label, ok, preserve := parseNeutralizedLocalMarkdownLinkPrefix(text[last : index-2])
	if preserve {
		return neutralizedLocalMarkdownRewrite{
			text:    text[last : close+run+1],
			end:     close + run + 1,
			changed: false,
		}, true
	}
	if !ok {
		return neutralizedLocalMarkdownRewrite{}, false
	}
	rewrittenHead, headChanged, headErrs := p.rewriteMarkdownLinksPlain(
		ctx,
		req,
		principals,
		runtime,
		scopeKey,
		rewrittenTargets,
		head,
		baseOffset+last,
	)
	replacement, linkChanged, linkErrs := p.rewritePreviewReferenceTarget(
		ctx,
		req,
		principals,
		runtime,
		scopeKey,
		rewrittenTargets,
		rawTarget,
		baseOffset+index+run+trimStart,
		baseOffset+index+run+trimStart+len(rawTarget),
	)
	var builder strings.Builder
	builder.WriteString(rewrittenHead)
	if linkChanged {
		builder.WriteByte('[')
		builder.WriteString(label)
		builder.WriteString("](")
		builder.WriteString(replacement)
		builder.WriteByte(')')
	} else {
		builder.WriteString(label)
		builder.WriteString(text[index-2 : close+run+1])
	}
	return neutralizedLocalMarkdownRewrite{
		text:    builder.String(),
		errs:    append(headErrs, linkErrs...),
		end:     close + run + 1,
		changed: headChanged || linkChanged,
	}, true
}
