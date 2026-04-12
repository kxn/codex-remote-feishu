package editor

import "encoding/json"

func decodeVSCodeSettings(raw []byte) (map[string]any, error) {
	settings := map[string]any{}
	if len(raw) == 0 {
		return settings, nil
	}
	normalized := stripVSCodeJSONTrailingCommas(stripVSCodeJSONComments(raw))
	normalized = normalizeVSCodeJSONStringEscapes(normalized)
	if err := json.Unmarshal(normalized, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

func normalizeVSCodeJSONStringEscapes(raw []byte) []byte {
	out := make([]byte, 0, len(raw)+8)
	inString := false
	escape := false

	for i := 0; i < len(raw); i++ {
		current := raw[i]
		if !inString {
			out = append(out, current)
			if current == '"' {
				inString = true
				escape = false
			}
			continue
		}
		if escape {
			out = append(out, current)
			escape = false
			continue
		}
		switch current {
		case '\\':
			if isValidJSONEscape(raw, i) {
				out = append(out, current)
				escape = true
				continue
			}
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, current)
			inString = false
		default:
			out = append(out, current)
		}
	}
	return out
}

func isValidJSONEscape(raw []byte, slashIndex int) bool {
	if slashIndex < 0 || slashIndex+1 >= len(raw) {
		return false
	}
	switch raw[slashIndex+1] {
	case '"', '\\', '/':
		return true
	case 'u':
		if slashIndex+5 >= len(raw) {
			return false
		}
		return isHex(raw[slashIndex+2]) &&
			isHex(raw[slashIndex+3]) &&
			isHex(raw[slashIndex+4]) &&
			isHex(raw[slashIndex+5])
	default:
		return false
	}
}

func isHex(value byte) bool {
	switch {
	case value >= '0' && value <= '9':
		return true
	case value >= 'a' && value <= 'f':
		return true
	case value >= 'A' && value <= 'F':
		return true
	default:
		return false
	}
}

func stripVSCodeJSONComments(raw []byte) []byte {
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

func stripVSCodeJSONTrailingCommas(raw []byte) []byte {
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
				if isVSCodeJSONWhitespace(out[i]) {
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

func isVSCodeJSONWhitespace(value byte) bool {
	switch value {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}
