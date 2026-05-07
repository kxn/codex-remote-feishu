package control

import "strings"

func feishuAdminAutostartSupportedPlatform(goos string) bool {
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case "linux", "darwin":
		return true
	default:
		return false
	}
}
