package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func shouldPreserveOpenCodeAdmissionRef(previous, current surfaceresume.Entry, clearResumeTarget bool) bool {
	if clearResumeTarget || previous.OpenCodeAdmissionRef == nil || strings.TrimSpace(current.ResumeThreadID) == "" ||
		strings.TrimSpace(previous.ResumeThreadID) != strings.TrimSpace(current.ResumeThreadID) {
		return false
	}
	profileID := state.NormalizeOpenCodeProfileID(current.OpenCodeProfileID)
	return previous.OpenCodeAdmissionRef.ProfileRef.ID == profileID
}
