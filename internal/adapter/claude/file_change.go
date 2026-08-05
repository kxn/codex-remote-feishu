package claude

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func claudeToolFileChanges(metadata map[string]any) []agentproto.FileChangeRecord {
	if len(metadata) == 0 {
		return nil
	}
	if records := claudeStructuredPatchFileChanges(metadata); len(records) != 0 {
		return records
	}
	path := strings.TrimSpace(xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(metadata["filePath"]),
		xutil.LookupStringFromAny(metadata["file_path"]),
		xutil.LookupStringFromAny(metadata["path"]),
	))
	if path == "" {
		return nil
	}
	oldString := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(metadata["oldString"]),
		xutil.LookupStringFromAny(metadata["old_string"]),
	)
	newString := xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(metadata["newString"]),
		xutil.LookupStringFromAny(metadata["new_string"]),
	)
	record := agentproto.FileChangeRecord{
		Path: path,
		Kind: claudeFileChangeKindFromMetadata(metadata, oldString, newString),
		Diff: buildClaudeFileChangeDiff(metadata, path, oldString, newString),
	}
	return []agentproto.FileChangeRecord{record}
}

func claudeStructuredPatchFileChanges(metadata map[string]any) []agentproto.FileChangeRecord {
	records := xutil.MapsFromAny(metadata["structuredPatchRecords"])
	if len(records) == 0 {
		return nil
	}
	path := strings.TrimSpace(xutil.FirstNonEmpty(
		xutil.LookupStringFromAny(metadata["filePath"]),
		xutil.LookupStringFromAny(metadata["file_path"]),
		xutil.LookupStringFromAny(metadata["path"]),
	))
	if path == "" {
		return nil
	}
	out := make([]agentproto.FileChangeRecord, 0, len(records))
	for _, record := range records {
		newLines := xutil.LookupIntFromAny(record["newLines"])
		oldLines := xutil.LookupIntFromAny(record["oldLines"])
		kind := agentproto.FileChangeUpdate
		switch {
		case oldLines == 0 && newLines > 0:
			kind = agentproto.FileChangeAdd
		case oldLines > 0 && newLines == 0:
			kind = agentproto.FileChangeDelete
		}
		diff := buildClaudeStructuredPatchText(path, record)
		out = append(out, agentproto.FileChangeRecord{
			Path: path,
			Kind: kind,
			Diff: diff,
		})
	}
	return out
}

func claudeFileChangeKindFromMetadata(metadata map[string]any, oldString, newString string) agentproto.FileChangeKind {
	editMode := strings.ToLower(strings.TrimSpace(xutil.LookupStringFromAny(metadata["editMode"])))
	switch editMode {
	case "insert":
		return agentproto.FileChangeAdd
	case "delete":
		return agentproto.FileChangeDelete
	case "replace":
		return agentproto.FileChangeUpdate
	}
	changeType := strings.ToLower(strings.TrimSpace(xutil.LookupStringFromAny(metadata["changeType"])))
	switch changeType {
	case "create":
		return agentproto.FileChangeAdd
	case "delete", "remove":
		return agentproto.FileChangeDelete
	case "update", "replace":
		return agentproto.FileChangeUpdate
	}
	return claudeFileChangeKind(oldString, newString)
}

func claudeFileChangeKind(oldString, newString string) agentproto.FileChangeKind {
	oldString = strings.TrimSpace(oldString)
	newString = strings.TrimSpace(newString)
	switch {
	case oldString == "" && newString != "":
		return agentproto.FileChangeAdd
	case oldString != "" && newString == "":
		return agentproto.FileChangeDelete
	default:
		return agentproto.FileChangeUpdate
	}
}

func buildClaudeFileChangeDiff(metadata map[string]any, path, oldString, newString string) string {
	if diff := buildClaudeStructuredPatchDiff(metadata, path); diff != "" {
		return diff
	}
	if diff := strings.TrimSpace(xutil.LookupStringFromAny(metadata["structuredPatch"])); diff != "" {
		return diff
	}
	path = strings.TrimSpace(path)
	oldLabel := path
	newLabel := path
	if oldLabel == "" {
		oldLabel = "before"
	}
	if newLabel == "" {
		newLabel = "after"
	}
	switch claudeFileChangeKindFromMetadata(metadata, oldString, newString) {
	case agentproto.FileChangeAdd:
		return unifiedDiffText("/dev/null", newLabel, "", newString)
	case agentproto.FileChangeDelete:
		return unifiedDiffText(oldLabel, "/dev/null", oldString, "")
	default:
		return unifiedDiffText(oldLabel, newLabel, oldString, newString)
	}
}

func unifiedDiffText(oldLabel, newLabel, oldBody, newBody string) string {
	oldLines := splitClaudeDiffLines(oldBody)
	newLines := splitClaudeDiffLines(newBody)
	var buffer strings.Builder
	buffer.WriteString("--- ")
	buffer.WriteString(strings.TrimSpace(oldLabel))
	buffer.WriteString("\n")
	buffer.WriteString("+++ ")
	buffer.WriteString(strings.TrimSpace(newLabel))
	buffer.WriteString("\n")
	buffer.WriteString(fmt.Sprintf("@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines)))
	for _, line := range oldLines {
		buffer.WriteString("-")
		buffer.WriteString(line)
		buffer.WriteString("\n")
	}
	for _, line := range newLines {
		buffer.WriteString("+")
		buffer.WriteString(line)
		buffer.WriteString("\n")
	}
	return strings.TrimRight(buffer.String(), "\n")
}

func buildClaudeStructuredPatchDiff(metadata map[string]any, path string) string {
	changes := claudeStructuredPatchFileChanges(metadata)
	if len(changes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		diff := strings.TrimSpace(change.Diff)
		if diff == "" {
			continue
		}
		parts = append(parts, diff)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func buildClaudeStructuredPatchText(path string, record map[string]any) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "file"
	}
	oldStart := xutil.LookupIntFromAny(record["oldStart"])
	oldLines := xutil.LookupIntFromAny(record["oldLines"])
	newStart := xutil.LookupIntFromAny(record["newStart"])
	newLines := xutil.LookupIntFromAny(record["newLines"])
	if oldStart <= 0 {
		oldStart = 1
	}
	if newStart <= 0 {
		newStart = 1
	}
	var buffer strings.Builder
	buffer.WriteString("--- ")
	buffer.WriteString(path)
	buffer.WriteString("\n")
	buffer.WriteString("+++ ")
	buffer.WriteString(path)
	buffer.WriteString("\n")
	buffer.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldLines, newStart, newLines))
	for _, line := range stringSliceFromAny(record["lines"]) {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		buffer.WriteString(text)
		buffer.WriteString("\n")
	}
	return strings.TrimRight(buffer.String(), "\n")
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := xutil.LookupStringFromAny(item)
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func splitClaudeDiffLines(body string) []string {
	if body == "" {
		return nil
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	lines := strings.Split(body, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
