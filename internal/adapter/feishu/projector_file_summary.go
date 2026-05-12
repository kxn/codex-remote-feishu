package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/texttags"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/displaypath"
)

func formatFileChangePath(file control.FileChangeSummaryEntry, labels map[string]string) string {
	path := strings.TrimSpace(file.Path)
	movePath := strings.TrimSpace(file.MovePath)
	switch {
	case path != "" && movePath != "":
		return fmt.Sprintf("%s → %s", texttags.FormatNeutralTextTag(fileChangeDisplayLabel(path, labels)), texttags.FormatNeutralTextTag(fileChangeDisplayLabel(movePath, labels)))
	case path != "":
		return texttags.FormatNeutralTextTag(fileChangeDisplayLabel(path, labels))
	case movePath != "":
		return texttags.FormatNeutralTextTag(fileChangeDisplayLabel(movePath, labels))
	default:
		return texttags.FormatNeutralTextTag("(unknown)")
	}
}

func fileChangeDisplayLabels(files []control.FileChangeSummaryEntry) map[string]string {
	paths := make([]string, 0, len(files)*2)
	for _, file := range files {
		if path := displaypath.Normalize(file.Path); path != "" {
			paths = append(paths, path)
		}
		if movePath := displaypath.Normalize(file.MovePath); movePath != "" {
			paths = append(paths, movePath)
		}
	}
	return displaypath.FileLabels(paths)
}

func fileChangeDisplayLabel(path string, labels map[string]string) string {
	return displaypath.FileLabel(path, labels)
}

func formatFileChangeCountsMarkdown(added, removed int) string {
	return fmt.Sprintf("<font color='green'>+%d</font> <font color='red'>-%d</font>", added, removed)
}
