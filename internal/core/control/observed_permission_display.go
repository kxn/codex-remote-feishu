package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func HasObservedThreadAccess(summary PromptRouteSummary) bool {
	return strings.TrimSpace(ObservedThreadAccessDisplay(summary)) != ""
}

func ObservedThreadAccessDisplay(summary PromptRouteSummary) string {
	if summary.ObservedThreadPermission != nil {
		if access := agentproto.NormalizeAccessMode(summary.ObservedThreadPermission.ProjectedAccessMode); access != "" {
			return agentproto.DisplayAccessModeShort(access)
		}
		if nativeMode := strings.TrimSpace(summary.ObservedThreadPermission.NativeMode); nativeMode != "" {
			return nativeMode + "（当前无本地精确映射）"
		}
	}
	if access := agentproto.NormalizeAccessMode(summary.ObservedThreadAccessMode); access != "" {
		return agentproto.DisplayAccessModeShort(access)
	}
	return ""
}
