package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func normalizeResumeThreadTitle(title, threadID, threadCWD, workspaceKey string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}

	workspaceShort := state.WorkspaceShortName(state.ResolveWorkspaceKey(threadCWD, workspaceKey))
	shortID := control.ShortenThreadID(strings.TrimSpace(threadID))

	if shortID != "" {
		suffix := " · " + shortID
		for {
			switch {
			case title == shortID:
				return ""
			case strings.HasSuffix(title, suffix):
				title = strings.TrimSpace(strings.TrimSuffix(title, suffix))
			default:
				goto stripPrefix
			}
		}
	}

stripPrefix:
	if workspaceShort == "" {
		return title
	}
	prefix := workspaceShort + " · "
	for {
		switch {
		case title == workspaceShort:
			return ""
		case strings.HasPrefix(title, prefix):
			title = strings.TrimSpace(strings.TrimPrefix(title, prefix))
		default:
			return title
		}
	}
}

func storedResumeThreadTitle(snapshotTitle, threadID, threadCWD, workspaceKey string, threadName string) string {
	if raw := normalizeResumeThreadTitle(threadName, threadID, threadCWD, workspaceKey); raw != "" {
		return raw
	}
	if raw := normalizeResumeThreadTitle(snapshotTitle, threadID, threadCWD, workspaceKey); raw != "" {
		return raw
	}
	return strings.TrimSpace(threadID)
}
