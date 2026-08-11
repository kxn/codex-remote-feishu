package xutil

import (
	"path/filepath"
	"strings"
)

// BoolString renders value as "true" or "false", consolidating the copies
// previously living in install/service.go and config/envfile.go.
func BoolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// CleanPath trims and cleans path with filepath.Clean, returning "" for empty
// or whitespace-only input. It consolidates the cleanNonEmpty copies in
// vscodeshim and shim.
func CleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// MetadataString returns the trimmed string value for key in metadata, or ""
// when the key is absent or holds a non-string value. It consolidates the
// metadataString copies in adapter/acp, orchestrator and execprogress.
func MetadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

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
