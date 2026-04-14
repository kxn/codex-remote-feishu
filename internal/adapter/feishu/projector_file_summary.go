package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func formatFileChangePath(file control.FileChangeSummaryEntry, labels map[string]string) string {
	path := strings.TrimSpace(file.Path)
	movePath := strings.TrimSpace(file.MovePath)
	switch {
	case path != "" && movePath != "":
		return fmt.Sprintf("%s → %s", formatNeutralTextTag(fileChangeDisplayLabel(path, labels)), formatNeutralTextTag(fileChangeDisplayLabel(movePath, labels)))
	case path != "":
		return formatNeutralTextTag(fileChangeDisplayLabel(path, labels))
	case movePath != "":
		return formatNeutralTextTag(fileChangeDisplayLabel(movePath, labels))
	default:
		return formatNeutralTextTag("(unknown)")
	}
}

func fileChangeDisplayLabels(files []control.FileChangeSummaryEntry) map[string]string {
	paths := make([]string, 0, len(files)*2)
	for _, file := range files {
		if path := normalizeFileSummaryPath(file.Path); path != "" {
			paths = append(paths, path)
		}
		if movePath := normalizeFileSummaryPath(file.MovePath); movePath != "" {
			paths = append(paths, movePath)
		}
	}
	return shortestUniquePathSuffixes(paths)
}

func fileChangeDisplayLabel(path string, labels map[string]string) string {
	normalized := normalizeFileSummaryPath(path)
	if normalized == "" {
		return ""
	}
	if label := strings.TrimSpace(labels[normalized]); label != "" {
		return label
	}
	return clampDisplayPath(normalized)
}

func shortestUniquePathSuffixes(paths []string) map[string]string {
	unique := uniqueNormalizedPaths(paths)
	if len(unique) == 0 {
		return map[string]string{}
	}
	resolved := make(map[string]string, len(unique))
	maxDepth := 0
	for _, path := range unique {
		if depth := len(strings.Split(path, "/")); depth > maxDepth {
			maxDepth = depth
		}
	}
	for depth := 1; depth <= maxDepth; depth++ {
		counts := map[string]int{}
		for _, path := range unique {
			if resolved[path] != "" {
				continue
			}
			suffix := pathSuffix(path, depth)
			counts[suffix]++
		}
		for _, path := range unique {
			if resolved[path] != "" {
				continue
			}
			suffix := pathSuffix(path, depth)
			if counts[suffix] == 1 {
				resolved[path] = clampDisplayPath(suffix)
			}
		}
	}
	for _, path := range unique {
		if resolved[path] == "" {
			resolved[path] = clampDisplayPath(path)
		}
	}
	return resolved
}

func uniqueNormalizedPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		normalized := normalizeFileSummaryPath(path)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func normalizeFileSummaryPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.Trim(path, "/")
	return path
}

func pathSuffix(path string, depth int) string {
	parts := strings.Split(normalizeFileSummaryPath(path), "/")
	if depth >= len(parts) {
		return strings.Join(parts, "/")
	}
	return strings.Join(parts[len(parts)-depth:], "/")
}

func clampDisplayPath(path string) string {
	path = normalizeFileSummaryPath(path)
	const maxLen = 36
	if len(path) <= maxLen {
		return path
	}
	if idx := strings.Index(path, "/"); idx >= 0 {
		tail := path
		if len(tail) > maxLen-4 {
			tail = tail[len(tail)-(maxLen-4):]
		}
		return ".../" + strings.TrimLeft(tail, "/")
	}
	return path[len(path)-maxLen:]
}

func formatFileChangeCountsMarkdown(added, removed int) string {
	return fmt.Sprintf("<font color='green'>+%d</font> <font color='red'>-%d</font>", added, removed)
}
