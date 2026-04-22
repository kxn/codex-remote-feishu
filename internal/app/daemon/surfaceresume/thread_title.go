package surfaceresume

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func NormalizeThreadTitle(title, threadID, threadCWD, workspaceKey string) string {
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

func StoredThreadTitle(snapshotTitle, threadID, threadCWD, workspaceKey, threadName string) string {
	if raw := NormalizeThreadTitle(threadName, threadID, threadCWD, workspaceKey); raw != "" {
		return raw
	}
	if raw := NormalizeThreadTitle(snapshotTitle, threadID, threadCWD, workspaceKey); raw != "" {
		return raw
	}
	return strings.TrimSpace(threadID)
}
