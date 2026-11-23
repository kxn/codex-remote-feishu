package preview

import (
	"errors"
	"strings"
	"unicode"

	"github.com/kxn/codex-remote-feishu/internal/core/markdown"
)

type rewriteReference func(target string, start, end int) (string, bool, []string)

// HasFinalBlockCandidates uses the same Markdown traversal as publication,
// without reading files, resolving grants, or opening a network connection.
func HasFinalBlockCandidates(text string) bool {
	found := false
	check := rewriteReference(func(target string, _, _ int) (string, bool, []string) {
		if _, ok := normalizePreviewReferenceTarget(target); ok {
			found = true
		}
		return target, false, nil
	})
	_, _, _ = check.markdown(text)
	return found
}

func (resolve rewriteReference) markdown(text string) (string, bool, error) {
	segments := markdown.SplitFenceSegments(text)
	if len(segments) == 0 {
		return text, false, nil
	}

	var errs []string

	var builder strings.Builder
	changed := false
	offset := 0
	for _, segment := range segments {
		if segment.Fenced {
			builder.WriteString(segment.Text)
			offset += len(segment.Text)
			continue
		}
		rewrittenSegment, segmentChanged, segmentErrs := resolve.rewriteMarkdownLinksInline(
			segment.Text,
			offset,
		)
		builder.WriteString(rewrittenSegment)
		if segmentChanged {
			changed = true
		}
		if len(segmentErrs) > 0 {
			errs = append(errs, segmentErrs...)
		}
		offset += len(segment.Text)
	}

	var rewriteErr error
	if len(errs) > 0 {
		rewriteErr = errors.New(strings.Join(errs, "; "))
	}
	return builder.String(), changed, rewriteErr
}

func (resolve rewriteReference) rewriteMarkdownLinksInline(
	text string,
	baseOffset int,
) (string, bool, []string) {
	if text == "" {
		return "", false, nil
	}

	var (
		builder strings.Builder
		errs    []string
		changed bool
	)
	last := 0
	for i := 0; i < len(text); {
		if rewrittenLink, ok := resolve.tryRewriteNeutralizedLocalMarkdownLink(
			text,
			last,
			i,
			baseOffset,
		); ok {
			builder.WriteString(rewrittenLink.text)
			if rewrittenLink.changed {
				changed = true
			}
			if len(rewrittenLink.errs) > 0 {
				errs = append(errs, rewrittenLink.errs...)
			}
			i = rewrittenLink.end
			last = i
			continue
		}
		if text[i] != '`' {
			i++
			continue
		}
		run := markdown.ConsecutiveByteRun(text, i, '`')
		close := markdown.ClosingBacktickRun(text, i+run, run)
		if close < 0 {
			break
		}
		rewritten, rewrittenChanged, rewrittenErrs := resolve.rewriteMarkdownLinksPlain(
			text[last:i],
			baseOffset+last,
		)
		builder.WriteString(rewritten)
		if rewrittenChanged {
			changed = true
		}
		if len(rewrittenErrs) > 0 {
			errs = append(errs, rewrittenErrs...)
		}
		inlineRewritten, inlineChanged, inlineErrs := resolve.rewriteMarkdownLinksCodeSpan(
			text[i:close+run],
			text[i+run:close],
			baseOffset+i+run,
		)
		builder.WriteString(inlineRewritten)
		if inlineChanged {
			changed = true
		}
		if len(inlineErrs) > 0 {
			errs = append(errs, inlineErrs...)
		}
		i = close + run
		last = i
	}

	rewritten, rewrittenChanged, rewrittenErrs := resolve.rewriteMarkdownLinksPlain(
		text[last:],
		baseOffset+last,
	)
	builder.WriteString(rewritten)
	if rewrittenChanged {
		changed = true
	}
	if len(rewrittenErrs) > 0 {
		errs = append(errs, rewrittenErrs...)
	}
	return builder.String(), changed, errs
}

func (resolve rewriteReference) rewriteMarkdownLinksCodeSpan(
	rawSpan string,
	text string,
	baseOffset int,
) (string, bool, []string) {
	if text == "" {
		return rawSpan, false, nil
	}
	trimStart, trimEnd := trimMarkdownInlineSpaceBounds(text)
	if trimStart >= trimEnd {
		return rawSpan, false, nil
	}
	trimmed := text[trimStart:trimEnd]
	if end, label, rawTarget, ok := markdown.ParseMarkdownLinkAt(trimmed, 0); ok && end == len(trimmed) {
		replacement, rewrittenChanged, rewrittenErrs := resolve(
			rawTarget,
			baseOffset+trimStart+len(label)+3,
			baseOffset+trimStart+len(label)+3+len(rawTarget),
		)
		if !rewrittenChanged {
			return rawSpan, false, rewrittenErrs
		}
		return "[" + label + "](" + replacement + ")", true, rewrittenErrs
	}
	rawTarget, display, ok := parseStandalonePreviewReferenceWhole(trimmed)
	if !ok {
		return rawSpan, false, nil
	}
	replacement, rewrittenChanged, rewrittenErrs := resolve(
		rawTarget,
		baseOffset+trimStart,
		baseOffset+trimStart+len(rawTarget),
	)
	if !rewrittenChanged {
		return rawSpan, false, rewrittenErrs
	}
	return "[" + display + "](" + replacement + ")", true, rewrittenErrs
}

func (resolve rewriteReference) rewriteMarkdownLinksPlain(
	text string,
	baseOffset int,
) (string, bool, []string) {
	if text == "" {
		return "", false, nil
	}

	var (
		builder strings.Builder
		errs    []string
		changed bool
	)
	last := 0
	for i := 0; i < len(text); {
		// A bare URL is one token. Its host and path must never be
		// reconsidered as local file references after a colon or dot.
		if strings.HasPrefix(text[i:], "://") {
			end := strings.IndexFunc(text[i+3:], func(r rune) bool {
				return unicode.IsSpace(r) || isStandalonePreviewTerminator(r)
			})
			if end < 0 {
				break
			}
			i += 3 + end
			continue
		}
		if text[i] == '[' {
			end, label, rawTarget, ok := markdown.ParseMarkdownLinkAt(text, i)
			if ok {
				builder.WriteString(text[last:i])
				replacement, rewrittenChanged, rewrittenErrs := resolve(
					rawTarget,
					baseOffset+i+len(label)+3,
					baseOffset+i+len(label)+3+len(rawTarget),
				)
				if rewrittenChanged {
					builder.WriteByte('[')
					builder.WriteString(label)
					builder.WriteString("](")
					builder.WriteString(replacement)
					builder.WriteByte(')')
					changed = true
				} else {
					builder.WriteString(text[i:end])
				}
				if len(rewrittenErrs) > 0 {
					errs = append(errs, rewrittenErrs...)
				}
				i = end
				last = i
				continue
			}
		}
		end, rawTarget, display, ok := parseStandalonePreviewReferenceAt(text, i)
		if !ok {
			i++
			continue
		}
		builder.WriteString(text[last:i])
		replacement, rewrittenChanged, rewrittenErrs := resolve(
			rawTarget,
			baseOffset+i,
			baseOffset+i+len(rawTarget),
		)
		if rewrittenChanged {
			builder.WriteByte('[')
			builder.WriteString(display)
			builder.WriteString("](")
			builder.WriteString(replacement)
			builder.WriteByte(')')
			changed = true
		} else {
			builder.WriteString(text[i:end])
		}
		if len(rewrittenErrs) > 0 {
			errs = append(errs, rewrittenErrs...)
		}
		i = end
		last = i
	}
	builder.WriteString(text[last:])
	return builder.String(), changed, errs
}
