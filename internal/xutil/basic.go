package xutil

import "strings"

// FirstNonEmpty returns the first value whose trimmed form is non-empty,
// trimmed. It returns "" when all values are empty or whitespace only.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// MaxInt returns the larger of a and b.
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// BoolPtr returns a pointer to a copy of value.
func BoolPtr(value bool) *bool {
	return &value
}

// BoolValue dereferences a bool pointer, returning false for nil. It is the
// inverse of BoolPtr; the two were conflated under one boolPtr name in
// config/install (ref) and the feishu adapter (deref).
func BoolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

// ContainsString reports whether values contains target.
func ContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
