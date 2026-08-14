package xutil

// JSONC normalization helpers.
//
// These were previously copy-pasted as stripVSCodeJSONComments /
// stripVSCodeJSONTrailingCommas (internal/adapter/editor) and
// stripJSONComments / stripJSONTrailingCommas (internal/app/opencodeprofile).
// The two copies had diverged only in naming; this is the shared
// implementation.

// StripJSONComments removes // line comments and /* */ block comments from a
// JSONC document. Everything inside string literals is preserved verbatim,
// including escaped quotes and backslashes. Newlines inside block comments are
// kept so the output stays line-aligned with the input.
func StripJSONComments(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	inString := false
	escape := false
	lineComment := false
	blockComment := false
	for i := 0; i < len(raw); i++ {
		current := raw[i]
		if lineComment {
			if current == '\n' || current == '\r' {
				lineComment = false
				out = append(out, current)
			}
			continue
		}
		if blockComment {
			if current == '\n' || current == '\r' {
				out = append(out, current)
				continue
			}
			if current == '*' && i+1 < len(raw) && raw[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if inString {
			out = append(out, current)
			if escape {
				escape = false
				continue
			}
			switch current {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			out = append(out, current)
			continue
		}
		if current == '/' && i+1 < len(raw) {
			switch raw[i+1] {
			case '/':
				lineComment = true
				i++
				continue
			case '*':
				blockComment = true
				i++
				continue
			}
		}
		out = append(out, current)
	}
	return out
}

// StripJSONTrailingCommas removes commas that immediately precede a closing
// brace or bracket (outside string literals), e.g. {"a":1,} or [1,2,].
func StripJSONTrailingCommas(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	inString := false
	escape := false
	for _, current := range raw {
		if inString {
			out = append(out, current)
			if escape {
				escape = false
				continue
			}
			switch current {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		switch current {
		case '"':
			inString = true
			out = append(out, current)
		case '}', ']':
			for i := len(out) - 1; i >= 0; i-- {
				if isJSONWhitespace(out[i]) {
					continue
				}
				if out[i] == ',' {
					out = append(out[:i], out[i+1:]...)
				}
				break
			}
			out = append(out, current)
		default:
			out = append(out, current)
		}
	}
	return out
}

// NormalizeJSONC strips comments and trailing commas so the result can be fed
// to encoding/json. It is the fixed combination both call sites used.
func NormalizeJSONC(raw []byte) []byte {
	return StripJSONTrailingCommas(StripJSONComments(raw))
}

func isJSONWhitespace(value byte) bool {
	switch value {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}
