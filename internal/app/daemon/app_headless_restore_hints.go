package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (a *App) HeadlessRestoreHint(surfaceID string) *surfaceresume.HeadlessRestoreHint {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.surfaceResumeRuntime.store == nil {
		return nil
	}
	entry, ok := a.surfaceResumeRuntime.store.Get(surfaceID)
	if !ok {
		return nil
	}
	hint, ok := headlessRestoreHintFromSurfaceResumeEntry(entry)
	if !ok {
		return nil
	}
	copy := hint
	return &copy
}

func sameHeadlessRestoreHintContent(left, right surfaceresume.HeadlessRestoreHint) bool {
	return strings.TrimSpace(left.SurfaceSessionID) == strings.TrimSpace(right.SurfaceSessionID) &&
		strings.TrimSpace(left.GatewayID) == strings.TrimSpace(right.GatewayID) &&
		strings.TrimSpace(left.ChatID) == strings.TrimSpace(right.ChatID) &&
		strings.TrimSpace(left.ActorUserID) == strings.TrimSpace(right.ActorUserID) &&
		strings.TrimSpace(left.ThreadID) == strings.TrimSpace(right.ThreadID) &&
		strings.TrimSpace(left.ThreadTitle) == strings.TrimSpace(right.ThreadTitle) &&
		strings.TrimSpace(left.ThreadCWD) == strings.TrimSpace(right.ThreadCWD)
}

func headlessRestoreHintFromSurfaceResumeEntry(entry surfaceresume.Entry) (surfaceresume.HeadlessRestoreHint, bool) {
	if !entry.ResumeHeadless {
		return surfaceresume.HeadlessRestoreHint{}, false
	}
	hint := surfaceresume.HeadlessRestoreHint{
		SurfaceSessionID: strings.TrimSpace(entry.SurfaceSessionID),
		GatewayID:        strings.TrimSpace(entry.GatewayID),
		ChatID:           strings.TrimSpace(entry.ChatID),
		ActorUserID:      strings.TrimSpace(entry.ActorUserID),
		ThreadID:         strings.TrimSpace(entry.ResumeThreadID),
		ThreadTitle:      strings.TrimSpace(entry.ResumeThreadTitle),
		ThreadCWD:        state.NormalizeWorkspaceKey(entry.ResumeThreadCWD),
	}
	return surfaceresume.NormalizeHeadlessRestoreHint(hint)
}
