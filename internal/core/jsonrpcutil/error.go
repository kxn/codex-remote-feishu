package jsonrpcutil

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

// ExtractErrorMessage returns a human-readable JSON-RPC error message.
// It prefers `error.message` and falls back to top-level `error` when that
// field is a string.
func ExtractErrorMessage(message map[string]any) string {
	if message == nil {
		return ""
	}
	errorMap, _ := message["error"].(map[string]any)
	return xutil.FirstNonEmpty(
		lookupString(errorMap["message"]),
		lookupString(message["error"]),
	)
}

func lookupString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
