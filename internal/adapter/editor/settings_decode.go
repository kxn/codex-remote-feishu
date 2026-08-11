package editor

import (
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func decodeVSCodeSettings(raw []byte) (map[string]any, error) {
	settings := map[string]any{}
	if len(raw) == 0 {
		return settings, nil
	}
	normalized := xutil.NormalizeJSONC(raw)
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
