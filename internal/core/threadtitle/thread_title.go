package threadtitle

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const (
	DefaultDisplayLimit = 40
	UnnamedDisplayName  = "未命名会话"
)

type Context struct {
	ThreadID     string
	ThreadCWD    string
	WorkspaceKey string
}

func RawSemanticTitle(thread *state.ThreadRecord) string {
	if thread == nil {
		return ""
	}
	if name := normalizedThreadName(thread.Name); name != "" {
		return name
	}
	if recent := LastUserSnippet(thread, 0); recent != "" {
		return recent
	}
	if first := FirstUserSnippet(thread, 0); first != "" {
		return first
	}
	return ""
}

func DisplayBody(thread *state.ThreadRecord, limit int) string {
	if raw := RawSemanticTitle(thread); raw != "" {
		return truncateText(raw, limit)
	}
	return UnnamedDisplayName
}

func DisplayTitle(inst *state.InstanceRecord, thread *state.ThreadRecord, limit int) string {
	body := DisplayBody(thread, limit)
	if body == "" {
		return ""
	}
	prefix := WorkspaceLabel(inst, thread)
	if prefix == "" {
		return body
	}
	return fmt.Sprintf("%s · %s", prefix, body)
}

func WorkspaceLabel(inst *state.InstanceRecord, thread *state.ThreadRecord) string {
	if thread != nil {
		if short := state.WorkspaceShortName(thread.CWD); short != "" {
			return short
		}
	}
	if inst != nil {
		if short := state.WorkspaceShortName(inst.WorkspaceKey); short != "" {
			return short
		}
		if short := state.WorkspaceShortName(inst.WorkspaceRoot); short != "" {
			return short
		}
		if short := strings.TrimSpace(inst.ShortName); short != "" {
			return short
		}
		if short := strings.TrimSpace(inst.DisplayName); short != "" {
			return short
		}
	}
	return ""
}

func FirstUserSnippet(thread *state.ThreadRecord, limit int) string {
	if thread == nil {
		return ""
	}
	return truncateText(previewLine(thread.FirstUserMessage), limit)
}

func LastUserSnippet(thread *state.ThreadRecord, limit int) string {
	if thread == nil {
		return ""
	}
	return truncateText(previewLine(thread.LastUserMessage), limit)
}

func NormalizeStoredInput(title string, ctx Context) string {
	title = normalizeText(title)
	if title == "" {
		return ""
	}

	if shortID := control.ShortenThreadID(strings.TrimSpace(ctx.ThreadID)); shortID != "" {
		suffix := " · " + shortID
		for {
			switch {
			case title == shortID:
				return ""
			case strings.HasSuffix(title, suffix):
				title = normalizeText(strings.TrimSuffix(title, suffix))
			default:
				goto stripWorkspacePrefix
			}
		}
	}

stripWorkspacePrefix:
	if workspaceShort := state.WorkspaceShortName(state.ResolveWorkspaceKey(ctx.ThreadCWD, ctx.WorkspaceKey)); workspaceShort != "" {
		prefix := workspaceShort + " · "
		for {
			switch {
			case title == workspaceShort:
				return ""
			case strings.HasPrefix(title, prefix):
				title = normalizeText(strings.TrimPrefix(title, prefix))
			default:
				goto finalize
			}
		}
	}

finalize:
	title = normalizeText(title)
	switch {
	case title == "":
		return ""
	case title == UnnamedDisplayName:
		return ""
	case isPlaceholderThreadName(title):
		return ""
	default:
		return title
	}
}

func StoredTitle(snapshotTitle string, ctx Context, thread *state.ThreadRecord) string {
	if raw := RawSemanticTitle(thread); raw != "" {
		return raw
	}
	if raw := NormalizeStoredInput(snapshotTitle, ctx); raw != "" {
		return raw
	}
	return ""
}

func normalizedThreadName(name string) string {
	name = normalizeText(name)
	if name == "" || isPlaceholderThreadName(name) {
		return ""
	}
	return name
}

func isPlaceholderThreadName(name string) bool {
	switch strings.ToLower(normalizeText(name)) {
	case "", "新会话", "新聊天", "new chat", "new thread":
		return true
	default:
		return false
	}
}

func previewLine(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = normalizeText(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		return line
	}
	return normalizeText(text)
}

func normalizeText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func truncateText(text string, limit int) string {
	return xutil.TruncateRunes(text, limit, xutil.TruncateOptions{CollapseSpaces: true})
}
