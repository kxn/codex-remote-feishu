package pathcompare

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
)

// SameCleanPlatformPath reports whether left and right refer to the same path
// after canonicalization: Windows extended-length prefixes are stripped, the
// path is cleaned and slash-normalized, and Windows-like paths (drive-letter /
// UNC) compare case-insensitively regardless of the host GOOS.
func SameCleanPlatformPath(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return pathcanon.CompareKey(left) == pathcanon.CompareKey(right)
}
