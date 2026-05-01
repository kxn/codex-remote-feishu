package execprogress

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func Snapshot(progress *state.ExecCommandProgressRecord) *control.ExecCommandProgress {
	if progress == nil {
		return nil
	}
	entries := make([]control.ExecCommandProgressEntry, 0, len(progress.Entries))
	for _, entry := range progress.Entries {
		entries = append(entries, control.ExecCommandProgressEntry{
			ItemID:     entry.ItemID,
			Kind:       entry.Kind,
			Label:      entry.Label,
			Summary:    entry.Summary,
			Status:     entry.Status,
			FileChange: CloneFileChange(entry.FileChange),
			LastSeq:    entry.LastSeq,
		})
	}
	snapshot := &control.ExecCommandProgress{
		ThreadID:     progress.ThreadID,
		TurnID:       progress.TurnID,
		ItemID:       progress.ItemID,
		MessageID:    progress.MessageID,
		CardStartSeq: progress.CardStartSeq,
		Verbosity:    string(progress.Verbosity),
		Blocks:       Blocks(progress),
		Entries:      entries,
		Commands:     append([]string(nil), progress.Commands...),
		Command:      progress.Command,
		CWD:          progress.CWD,
		Status:       progress.Status,
	}
	snapshot.Timeline = control.BuildExecCommandProgressTimeline(*snapshot)
	return snapshot
}

func mapsFromAny(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if item != nil {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			record, _ := item.(map[string]any)
			if record != nil {
				out = append(out, record)
			}
		}
		return out
	default:
		return nil
	}
}

func CommandMetadata(event agentproto.Event) (string, string) {
	if event.Metadata == nil {
		return "", ""
	}
	command, _ := event.Metadata["command"].(string)
	cwd, _ := event.Metadata["cwd"].(string)
	return strings.TrimSpace(command), strings.TrimSpace(cwd)
}

func AppendCommandHistory(commands []string, command string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return commands
	}
	return append(commands, command)
}

func HasEntry(progress *state.ExecCommandProgressRecord, itemID, kind string) bool {
	if progress == nil {
		return false
	}
	itemID = strings.TrimSpace(itemID)
	kind = strings.TrimSpace(kind)
	if itemID == "" {
		return true
	}
	for _, entry := range progress.Entries {
		if entry.ItemID == itemID && (kind == "" || entry.Kind == kind) {
			return true
		}
	}
	return false
}

func UpsertEntry(progress *state.ExecCommandProgressRecord, entry state.ExecCommandProgressEntryRecord) {
	if progress == nil {
		return
	}
	entry.ItemID = strings.TrimSpace(entry.ItemID)
	entry.Kind = strings.TrimSpace(entry.Kind)
	entry.Label = strings.TrimSpace(entry.Label)
	entry.Summary = strings.TrimSpace(entry.Summary)
	entry.Status = strings.TrimSpace(entry.Status)
	if entry.Summary == "" {
		return
	}
	for i := range progress.Entries {
		current := &progress.Entries[i]
		if entry.ItemID != "" && current.ItemID == entry.ItemID {
			if entry.Kind != "" {
				current.Kind = entry.Kind
			}
			if entry.Label != "" {
				current.Label = entry.Label
			}
			if entry.Summary != "" {
				current.Summary = entry.Summary
			}
			if entry.Status != "" {
				current.Status = entry.Status
			}
			if entry.FileChange != nil {
				current.FileChange = entry.FileChange
			}
			if current.LastSeq == 0 {
				progress.LastVisibleSeq++
				current.LastSeq = progress.LastVisibleSeq
			}
			return
		}
	}
	progress.LastVisibleSeq++
	entry.LastSeq = progress.LastVisibleSeq
	progress.Entries = append(progress.Entries, entry)
}

func WebSearchEntry(metadata map[string]any, final bool) state.ExecCommandProgressEntryRecord {
	actionType := strings.TrimSpace(metadataString(metadata, "actionType"))
	query := strings.TrimSpace(metadataString(metadata, "query"))
	url := strings.TrimSpace(metadataString(metadata, "url"))
	pattern := strings.TrimSpace(metadataString(metadata, "pattern"))
	queries := metadataStringSlice(metadata, "queries")
	fallbackQuery := firstNonEmpty(query, firstNonEmptySlice(queries...))
	status := NormalizeStatus("", final)
	switch actionType {
	case "open_page":
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "打开网页",
			Summary: firstNonEmpty(url, fallbackWebSearchSummary(final)),
			Status:  status,
		}
	case "find_in_page":
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "页内查找",
			Summary: firstNonEmpty(formatFindInPageSummary(pattern, url), fallbackWebSearchSummary(final)),
			Status:  status,
		}
	case "search":
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "搜索",
			Summary: firstNonEmpty(fallbackQuery, fallbackWebSearchSummary(final)),
			Status:  status,
		}
	default:
		if final {
			return state.ExecCommandProgressEntryRecord{
				Kind:    "web_search",
				Label:   "搜索",
				Summary: firstNonEmpty(fallbackQuery, formatFindInPageSummary(pattern, url), url, "搜索完成"),
				Status:  status,
			}
		}
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "搜索",
			Summary: firstNonEmpty(fallbackQuery, "正在搜索网络"),
			Status:  status,
		}
	}
}

func UpsertDynamicToolProgressEntry(progress *state.ExecCommandProgressRecord, event agentproto.Event) (state.ExecCommandProgressEntryRecord, string, bool) {
	if progress == nil {
		return state.ExecCommandProgressEntryRecord{}, "", false
	}
	tool := strings.TrimSpace(metadataString(event.Metadata, "tool"))
	label := dynamicToolProgressLabel(tool)
	arguments := dynamicToolProgressArguments(event.Metadata)
	summary := strings.TrimSpace(dynamicToolProgressSummaryFromMetadata(event.Metadata))
	status := NormalizeDynamicToolProgressStatus(event)
	groupKey := dynamicToolGroupKey(progress, event.ItemID, tool)
	if groupKey == "" {
		return state.ExecCommandProgressEntryRecord{}, "", false
	}
	if progress.DynamicToolGroups == nil {
		progress.DynamicToolGroups = map[string]*state.DynamicToolProgressGroupRecord{}
	}
	if progress.DynamicToolItemGroup == nil {
		progress.DynamicToolItemGroup = map[string]string{}
	}
	if itemID := strings.TrimSpace(event.ItemID); itemID != "" {
		progress.DynamicToolItemGroup[itemID] = groupKey
	}
	group := progress.DynamicToolGroups[groupKey]
	if group == nil {
		group = &state.DynamicToolProgressGroupRecord{GroupKey: groupKey}
		progress.DynamicToolGroups[groupKey] = group
	}
	before := state.DynamicToolProgressGroupRecord{
		GroupKey: group.GroupKey,
		Tool:     group.Tool,
		Label:    group.Label,
		Args:     append([]string(nil), group.Args...),
		Summary:  group.Summary,
		Status:   group.Status,
	}
	if strings.TrimSpace(tool) != "" {
		group.Tool = strings.TrimSpace(tool)
	}
	if strings.TrimSpace(label) != "" {
		group.Label = strings.TrimSpace(label)
	}
	if len(arguments) != 0 {
		group.Args = appendUniquePreserveOrder(group.Args, arguments...)
	}
	if strings.TrimSpace(summary) != "" {
		group.Summary = strings.TrimSpace(summary)
	}
	if strings.TrimSpace(status) != "" {
		group.Status = strings.TrimSpace(status)
	}
	entry := state.ExecCommandProgressEntryRecord{
		ItemID:  groupKey,
		Kind:    "dynamic_tool_call",
		Label:   firstNonEmpty(group.Label, "工具"),
		Summary: buildDynamicToolProgressSummary(group),
		Status:  group.Status,
	}
	changed := group.Tool != before.Tool ||
		group.Label != before.Label ||
		group.Summary != before.Summary ||
		group.Status != before.Status ||
		!sameStringSlice(group.Args, before.Args)
	return entry, groupKey, changed
}

func NormalizeDynamicToolProgressStatus(event agentproto.Event) string {
	switch event.Kind {
	case agentproto.EventItemStarted:
		return "started"
	case agentproto.EventItemCompleted:
		status := strings.ToLower(strings.TrimSpace(event.Status))
		switch status {
		case "failed", "error":
			return "failed"
		case "completed", "complete", "ok", "success", "succeeded":
			return "completed"
		default:
			if success, ok := event.Metadata["success"].(bool); ok {
				if success {
					return "completed"
				}
				return "failed"
			}
			return "completed"
		}
	default:
		return ""
	}
}

func NormalizeStatus(status string, final bool) string {
	value := strings.ToLower(strings.TrimSpace(status))
	switch value {
	case "failed", "error":
		return "failed"
	case "interrupted", "cancelled", "canceled":
		return "interrupted"
	case "completed", "ok", "success", "succeeded":
		return "completed"
	case "inprogress", "in_progress", "running":
		return "running"
	case "":
		if final {
			return "completed"
		}
		return "running"
	default:
		if final {
			return value
		}
		return "running"
	}
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, current := range typed {
			if text := strings.TrimSpace(current); text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, current := range typed {
			if text := lookupStringFromAny(current); text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func lookupStringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmptySlice(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func dynamicToolGroupKey(progress *state.ExecCommandProgressRecord, itemID, tool string) string {
	normalizedTool := strings.ToLower(strings.TrimSpace(tool))
	if normalizedTool != "" {
		return "dynamic_tool_call::" + normalizedTool
	}
	itemID = strings.TrimSpace(itemID)
	if itemID != "" && progress != nil && progress.DynamicToolItemGroup != nil {
		if existing := strings.TrimSpace(progress.DynamicToolItemGroup[itemID]); existing != "" {
			return existing
		}
	}
	if itemID != "" {
		return "dynamic_tool_call::item::" + itemID
	}
	return ""
}

func dynamicToolProgressLabel(tool string) string {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "工具"
	}
	return tool
}

func dynamicToolProgressSummaryFromMetadata(metadata map[string]any) string {
	summary := strings.TrimSpace(metadataString(metadata, "text"))
	if summary != "" {
		return summary
	}
	if value := metadata["arguments"]; value != nil {
		return compactStructuredJSON(value)
	}
	return ""
}

func dynamicToolProgressArguments(metadata map[string]any) []string {
	if len(metadata) == 0 {
		return nil
	}
	value := metadata["arguments"]
	if value == nil {
		return nil
	}
	args := extractDynamicToolProgressArguments(value)
	if len(args) != 0 {
		return args
	}
	if compact := compactStructuredJSON(value); compact != "" {
		return []string{compact}
	}
	return nil
}

func extractDynamicToolProgressArguments(value any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	var walk func(key string, raw any)
	walk = func(key string, raw any) {
		switch typed := raw.(type) {
		case string:
			text := strings.TrimSpace(typed)
			if text == "" {
				return
			}
			if key != "" && !dynamicToolArgumentKeyRelevant(key) {
				return
			}
			if _, exists := seen[text]; exists {
				return
			}
			seen[text] = struct{}{}
			out = append(out, text)
		case []string:
			for _, current := range typed {
				walk(key, current)
			}
		case []any:
			for _, current := range typed {
				walk(key, current)
			}
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for current := range typed {
				keys = append(keys, current)
			}
			sort.Strings(keys)
			for _, current := range keys {
				walk(current, typed[current])
			}
		}
	}
	walk("", value)
	return out
}

func dynamicToolArgumentKeyRelevant(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "_", "")
	switch {
	case strings.Contains(normalized, "path"):
		return true
	case strings.Contains(normalized, "file"):
		return true
	case strings.Contains(normalized, "query"):
		return true
	case strings.Contains(normalized, "pattern"):
		return true
	case strings.Contains(normalized, "url"):
		return true
	case strings.Contains(normalized, "glob"):
		return true
	case strings.Contains(normalized, "target"):
		return true
	case strings.Contains(normalized, "text"):
		return true
	case strings.Contains(normalized, "name"):
		return true
	default:
		return false
	}
}

func compactStructuredJSON(value any) string {
	if value == nil {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func buildDynamicToolProgressSummary(group *state.DynamicToolProgressGroupRecord) string {
	if group == nil {
		return ""
	}
	summary := strings.TrimSpace(strings.Join(group.Args, " "))
	if summary == "" {
		summary = strings.TrimSpace(group.Summary)
	}
	if summary == "" {
		switch strings.ToLower(strings.TrimSpace(group.Status)) {
		case "failed":
			summary = "失败"
		case "completed":
			summary = "已完成"
		default:
			summary = "工作中"
		}
	}
	if strings.EqualFold(strings.TrimSpace(group.Status), "failed") && !strings.Contains(summary, "失败") {
		summary = summary + "（失败）"
	}
	return summary
}

func appendUniquePreserveOrder(base []string, values ...string) []string {
	if len(values) == 0 {
		return base
	}
	seen := map[string]struct{}{}
	for _, current := range base {
		text := strings.TrimSpace(current)
		if text == "" {
			continue
		}
		seen[text] = struct{}{}
	}
	for _, current := range values {
		text := strings.TrimSpace(current)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		base = append(base, text)
	}
	return base
}

func sameStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}

func formatFindInPageSummary(pattern, url string) string {
	pattern = strings.TrimSpace(pattern)
	url = strings.TrimSpace(url)
	switch {
	case pattern != "" && url != "":
		return fmt.Sprintf("%s @ %s", pattern, url)
	case pattern != "":
		return pattern
	case url != "":
		return url
	default:
		return ""
	}
}

func fallbackWebSearchSummary(final bool) string {
	if final {
		return "搜索完成"
	}
	return "正在搜索网络"
}
