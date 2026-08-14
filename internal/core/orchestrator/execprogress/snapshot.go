package execprogress

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func Snapshot(progress *state.ExecCommandProgressRecord) *control.ExecCommandProgress {
	if progress == nil {
		return nil
	}
	segments := make([]control.ExecCommandProgressSegment, 0, len(progress.Segments))
	for _, segment := range progress.Segments {
		segments = append(segments, control.ExecCommandProgressSegment{
			SegmentID: segment.SegmentID,
			MessageID: segment.MessageID,
			StartSeq:  segment.StartSeq,
			EndSeq:    segment.EndSeq,
		})
	}
	snapshot := &control.ExecCommandProgress{
		ThreadID:        progress.ThreadID,
		TurnID:          progress.TurnID,
		ItemID:          progress.ItemID,
		ActiveSegmentID: progress.ActiveSegmentID,
		Segments:        segments,
		Verbosity:       string(progress.Verbosity),
		Timeline:        Timeline(progress),
	}
	return snapshot
}

func visibleExecCommandProgressEntries(progress *state.ExecCommandProgressRecord) []state.ExecCommandProgressEntryRecord {
	if progress == nil {
		return nil
	}
	entries := make([]state.ExecCommandProgressEntryRecord, 0, len(progress.Entries)+1)
	for _, entry := range progress.Entries {
		entries = append(entries, CloneEntryRecord(entry))
	}
	if progress.Reasoning != nil && state.NormalizeSurfaceVerbosity(progress.Reasoning.VisibleVerbosity) == state.SurfaceVerbosityVerbose && strings.TrimSpace(progress.Reasoning.Text) != "" {
		status := "completed"
		if progress.Reasoning.Active {
			status = "running"
		}
		entries = append(entries, state.ExecCommandProgressEntryRecord{
			ItemID:    reasoningSlotItemID(progress.Reasoning),
			Kind:      "reasoning_summary",
			Summary:   strings.TrimSpace(progress.Reasoning.Text),
			Status:    status,
			Transient: true,
		})
	}
	return entries
}

func CloneEntryRecord(entry state.ExecCommandProgressEntryRecord) state.ExecCommandProgressEntryRecord {
	return state.ExecCommandProgressEntryRecord{
		ItemID:     entry.ItemID,
		Kind:       entry.Kind,
		Label:      entry.Label,
		Summary:    entry.Summary,
		Status:     entry.Status,
		FileChange: CloneFileChangeRecord(entry.FileChange),
		LastSeq:    entry.LastSeq,
		Transient:  entry.Transient,
	}
}

func CloneFileChangeRecord(change *state.ExecCommandProgressFileChangeRecord) *state.ExecCommandProgressFileChangeRecord {
	if change == nil {
		return nil
	}
	cloned := *change
	return &cloned
}

func CloneReasoningRecord(record *state.ExecCommandProgressReasoningRecord) *state.ExecCommandProgressReasoningRecord {
	if record == nil {
		return nil
	}
	cloned := *record
	return &cloned
}

func reasoningSlotItemID(record *state.ExecCommandProgressReasoningRecord) string {
	if record == nil || strings.TrimSpace(record.ItemID) == "" {
		return "reasoning_summary::latest"
	}
	return strings.TrimSpace(record.ItemID) + "::latest"
}

func CommandMetadata(event agentproto.Event) (string, string) {
	if event.Metadata == nil {
		return "", ""
	}
	command, _ := event.Metadata["command"].(string)
	cwd, _ := event.Metadata["cwd"].(string)
	return strings.TrimSpace(command), strings.TrimSpace(cwd)
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
	if entry.Summary == "" {
		return
	}
	if entry.Kind != "reasoning_summary" {
		FreezeReasoningForVisibleAction(progress)
	}
	progress.LastVisibleSeq++
	entry.LastSeq = progress.LastVisibleSeq
	progress.Entries = append(progress.Entries, entry)
}

func WebSearchEntry(metadata map[string]any, final bool) state.ExecCommandProgressEntryRecord {
	actionType := strings.TrimSpace(xutil.MetadataString(metadata, "actionType"))
	query := strings.TrimSpace(xutil.MetadataString(metadata, "query"))
	url := strings.TrimSpace(xutil.MetadataString(metadata, "url"))
	pattern := strings.TrimSpace(xutil.MetadataString(metadata, "pattern"))
	queries := metadataStringSlice(metadata, "queries")
	fallbackQuery := xutil.FirstNonEmpty(query, xutil.FirstNonEmpty(queries...))
	status := NormalizeStatus("", final)
	switch actionType {
	case "open_page":
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "打开网页",
			Summary: xutil.FirstNonEmpty(url, fallbackWebSearchSummary(final)),
			Status:  status,
		}
	case "find_in_page":
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "页内查找",
			Summary: xutil.FirstNonEmpty(formatFindInPageSummary(pattern, url), fallbackWebSearchSummary(final)),
			Status:  status,
		}
	case "search":
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "搜索",
			Summary: xutil.FirstNonEmpty(fallbackQuery, fallbackWebSearchSummary(final)),
			Status:  status,
		}
	default:
		if final {
			return state.ExecCommandProgressEntryRecord{
				Kind:    "web_search",
				Label:   "搜索",
				Summary: xutil.FirstNonEmpty(fallbackQuery, formatFindInPageSummary(pattern, url), url, "搜索完成"),
				Status:  status,
			}
		}
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "搜索",
			Summary: xutil.FirstNonEmpty(fallbackQuery, "正在搜索网络"),
			Status:  status,
		}
	}
}

func UpsertDynamicToolProgressEntry(progress *state.ExecCommandProgressRecord, event agentproto.Event) (state.ExecCommandProgressEntryRecord, string, bool) {
	if progress == nil {
		return state.ExecCommandProgressEntryRecord{}, "", false
	}
	tool := strings.TrimSpace(xutil.MetadataString(event.Metadata, "tool"))
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
	created := group == nil
	if group == nil {
		group = &state.DynamicToolProgressGroupRecord{GroupKey: groupKey}
		progress.DynamicToolGroups[groupKey] = group
	}
	beforeLabel := xutil.FirstNonEmpty(group.Label, "工具")
	beforeSummary := buildDynamicToolProgressSummary(group)
	beforeStatus := group.Status
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
	if group.ActiveItemIDs == nil {
		group.ActiveItemIDs = map[string]bool{}
	}
	itemID := strings.TrimSpace(event.ItemID)
	switch event.Kind {
	case agentproto.EventItemStarted:
		if itemID != "" {
			group.ActiveItemIDs[itemID] = true
		}
	case agentproto.EventItemCompleted:
		if itemID != "" {
			delete(group.ActiveItemIDs, itemID)
		}
		if status == "failed" {
			group.Failed = true
		}
	}
	if len(group.ActiveItemIDs) != 0 {
		group.Status = "started"
	} else if group.Failed {
		group.Status = "failed"
	} else if strings.TrimSpace(status) != "" {
		group.Status = strings.TrimSpace(status)
	}
	entry := state.ExecCommandProgressEntryRecord{
		ItemID:  groupKey,
		Kind:    "dynamic_tool_call",
		Label:   xutil.FirstNonEmpty(group.Label, "工具"),
		Summary: buildDynamicToolProgressSummary(group),
		Status:  group.Status,
	}
	changed := created || entry.Label != beforeLabel || entry.Summary != beforeSummary || entry.Status != beforeStatus
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
			if text := xutil.Stringify(current); text != "" {
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

func dynamicToolGroupKey(progress *state.ExecCommandProgressRecord, itemID, tool string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID != "" && progress != nil && progress.DynamicToolItemGroup != nil {
		if existing := strings.TrimSpace(progress.DynamicToolItemGroup[itemID]); existing != "" {
			return existing
		}
	}
	normalizedTool := strings.ToLower(strings.TrimSpace(tool))
	if normalizedTool != "" {
		baseKey := "dynamic_tool_call::" + normalizedTool
		if progress == nil {
			return baseKey
		}
		for i := len(progress.Entries) - 1; i >= 0; i-- {
			entry := progress.Entries[i]
			if entry.LastSeq != progress.LastVisibleSeq || entry.Kind != "dynamic_tool_call" {
				continue
			}
			group := progress.DynamicToolGroups[entry.ItemID]
			if group != nil && strings.EqualFold(strings.TrimSpace(group.Tool), normalizedTool) {
				return entry.ItemID
			}
			break
		}
		if progress.DynamicToolGroups == nil || progress.DynamicToolGroups[baseKey] == nil {
			return baseKey
		}
		for index := 2; ; index++ {
			candidate := baseKey + "::group::" + strconv.Itoa(index)
			if progress.DynamicToolGroups[candidate] == nil {
				return candidate
			}
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
	if value, ok := metadata["suppressFinalText"].(bool); !ok || !value {
		summary := strings.TrimSpace(xutil.MetadataString(metadata, "text"))
		if summary != "" {
			return summary
		}
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
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			return ""
		}
	case []any:
		if len(typed) == 0 {
			return ""
		}
	case []string:
		if len(typed) == 0 {
			return ""
		}
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
