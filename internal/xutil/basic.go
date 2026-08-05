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
