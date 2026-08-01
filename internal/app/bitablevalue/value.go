package bitablevalue

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func String(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"text", "name", "label", "title", "value", "id", "record_id", "recordId"} {
			if text := strings.TrimSpace(String(typed[key])); text != "" {
				return text
			}
		}
		if values := StringSlice(typed); len(values) > 0 {
			return strings.Join(values, "\n")
		}
		return ""
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(String(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []string:
		return strings.Join(typed, "\n")
	default:
		return fmt.Sprint(value)
	}
}

func Bool(value any) (bool, bool) {
	switch typed := value.(type) {
	case nil:
		return false, true
	case bool:
		return typed, true
	case int:
		return typed != 0, true
	case int32:
		return typed != 0, true
	case int64:
		return typed != 0, true
	case float32:
		return typed != 0, true
	case float64:
		return typed != 0, true
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed != 0, true
		}
		return false, false
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "", "0", "false", "off", "no", "unchecked", "停用":
			return false, true
		case "1", "true", "on", "yes", "checked", "启用":
			return true, true
		default:
			return false, false
		}
	case map[string]any:
		for _, key := range []string{"checked", "value", "text", "name", "label"} {
			if nested, ok := typed[key]; ok {
				if enabled, valid := Bool(nested); valid {
					return enabled, true
				}
			}
		}
		return false, false
	case []any:
		if len(typed) == 0 {
			return false, true
		}
		if len(typed) == 1 {
			return Bool(typed[0])
		}
		return false, false
	case []string:
		if len(typed) == 0 {
			return false, true
		}
		if len(typed) == 1 {
			return Bool(typed[0])
		}
		return false, false
	default:
		return Bool(fmt.Sprint(value))
	}
}

func StringSlice(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), typed...)
	case map[string]any:
		for _, key := range []string{"record_ids", "recordIds", "ids", "values"} {
			if values := StringSlice(typed[key]); len(values) > 0 {
				return values
			}
		}
		for _, key := range []string{"record_id", "recordId", "id", "value", "text", "name", "label"} {
			if text := strings.TrimSpace(String(typed[key])); text != "" {
				return []string{text}
			}
		}
		return nil
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if nested := StringSlice(item); len(nested) > 0 {
				values = append(values, nested...)
				continue
			}
			if text := strings.TrimSpace(String(item)); text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		text := strings.TrimSpace(String(value))
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func Int(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case map[string]any:
		for _, key := range []string{"value", "number", "text"} {
			if keyValue, ok := typed[key]; ok {
				return Int(keyValue)
			}
		}
		return 0
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		parsed, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		return parsed
	}
}
