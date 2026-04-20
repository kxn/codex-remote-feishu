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

func parseNeutralizedLocalMarkdownLinkPrefix(prefix string) (head, label string, ok bool) {
	prefixEnd := len(prefix)
	for start := 0; start < prefixEnd; {
		end, _, display, matched := parseStandalonePreviewReferenceAt(prefix, start)
		if matched && end == prefixEnd {
			return prefix[:start], display, true
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
		return "", "", false
	}
	return prefix[:labelStart], label, true
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
	head, label, ok := parseNeutralizedLocalMarkdownLinkPrefix(text[last : index-2])
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
