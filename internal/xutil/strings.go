package xutil

import "strings"

// TruncateOptions controls TruncateRunes behavior. Zero value matches the
// "plain rune cut + ..." shape used by most callers.
type TruncateOptions struct {
	// CollapseSpaces normalizes whitespace first: TrimSpace then fold any
	// runs of spaces into a single space (strings.Fields + Join).
	CollapseSpaces bool
	// Ellipsis is appended when truncation happens. Defaults to "...".
	Ellipsis string
	// ReserveEllipsis cuts at limit-len(ellipsis) runes so the total result
	// stays within limit. When false the result may exceed limit by the
	// ellipsis length.
	ReserveEllipsis bool
	// MinLimit clamps a positive limit below it up to MinLimit. Ignored when
	// <= 0. Used by callers that must keep at least a few runes.
	MinLimit int
	// NonPositiveLimitEmpty returns "" instead of the original text when
	// limit <= 0.
	NonPositiveLimitEmpty bool
	// TrimResult trims the truncated result (after appending the ellipsis).
	TrimResult bool
}

// TruncateRunes truncates text to at most limit runes, appending an ellipsis
// when truncation happened. It consolidates the rune-cut variants previously
// copy-pasted across feishu projector, orchestrator and threadtitle.
func TruncateRunes(text string, limit int, opts TruncateOptions) string {
	if opts.Ellipsis == "" {
		opts.Ellipsis = "..."
	}
	if opts.CollapseSpaces {
		text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	}
	if text == "" {
		return ""
	}
	if opts.MinLimit > 0 && limit < opts.MinLimit {
		limit = opts.MinLimit
	}
	if limit <= 0 {
		if opts.NonPositiveLimitEmpty {
			return ""
		}
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := limit
	if opts.ReserveEllipsis {
		cut = limit - len([]rune(opts.Ellipsis))
		if cut < 0 {
			cut = 0
		}
	}
	result := string(runes[:cut])
	if opts.TrimResult {
		result = strings.TrimSpace(result)
	}
	return result + opts.Ellipsis
}
