package agentproto

import (
	"strconv"
	"strings"
)

// SupportedPrivateOpenCodeVersion identifies the OpenCode v2 ACP implementation
// whose turn stream is gated by the exact durable inbox delivery identity.
func SupportedPrivateOpenCodeVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	nums := [3]int{}
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return false
		}
		n, e := strconv.Atoi(p)
		if e != nil || n < 0 {
			return false
		}
		nums[i] = n
	}
	return nums[0] == 2 && (nums[1] > 1 || nums[1] == 1 && nums[2] >= 4)
}
