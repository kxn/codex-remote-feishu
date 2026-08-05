// Package xutil provides small shared utility functions that were previously
// copy-pasted across several packages (adapter/claude, adapter/codex,
// core/orchestrator, claudesessionstore, app/daemon, ...). Consolidating them
// here gives a single source of truth for JSON lookup/clone helpers and basic
// value helpers.
//
// Migration note: each function here is the behavior superset of the copies it
// replaces. See issues #802 / #803 for the full inventory.
package xutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// LookupStringFromAny returns value as a string when it is one, or "" otherwise.
func LookupStringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

// Stringify converts a JSON value into a trimmed string. It handles nil, real
// strings, fmt.Stringer, and falls back to fmt.Sprint for every other value.
// This is the permissive variant that used to be copy-pasted in
// core/orchestrator and execprogress/snapshot.go; it intentionally differs
// from LookupStringFromAny, which only accepts real strings.
func Stringify(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

// LookupBoolFromAny returns value as a bool when it is one, or false otherwise.
func LookupBoolFromAny(value any) bool {
	current, _ := value.(bool)
	return current
}

// LookupIntFromAny returns value as an int when it is a supported numeric type,
// or 0 otherwise. Supported inputs: int, int32, int64, float32, float64.
// This is the union of the copies previously living in adapter/claude,
// adapter/codex and core/orchestrator.
func LookupIntFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

// CloneJSONValue deep-clones a JSON-compatible value. It handles nil, maps,
// []any and []map[string]any recursively; all other values are returned as-is.
// This is the superset previously implemented in core/orchestrator.
func CloneJSONValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = CloneJSONValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, CloneJSONValue(item))
		}
		return cloned
	case []map[string]any:
		cloned := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			record, _ := CloneJSONValue(item).(map[string]any)
			cloned = append(cloned, record)
		}
		return cloned
	default:
		return typed
	}
}

// CloneMap deep-clones a string-keyed map. An empty or nil input yields an
// empty (non-nil) map.
func CloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = CloneJSONValue(value)
	}
	return output
}

// MapsFromAny converts a JSON value into a slice of maps. Supported inputs:
// []map[string]any (deep-cloned) and []any (nil items are skipped, others are
// deep-cloned). Any other input yields nil.
func MapsFromAny(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, CloneMap(item))
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			object, _ := item.(map[string]any)
			if object != nil {
				out = append(out, CloneMap(object))
			}
		}
		return out
	default:
		return nil
	}
}

// CompactJSON marshals value into a compact JSON string. Nil yields ""; values
// that cannot be marshaled fall back to fmt.Sprintf("%v", value).
func CompactJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}
