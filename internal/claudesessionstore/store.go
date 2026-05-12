package claudesessionstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const (
	claudeSessionMetaReadBytes int64 = 64 * 1024
	claudeProjectDirNameMax          = 80
	claudeSessionTitleLimit          = 48
	claudeSessionPreviewLimit        = 72
)

type RuntimeStateSnapshot struct {
	SessionID            string
	CWD                  string
	Model                string
	AccessMode           string
	PlanMode             string
	NativePermissionMode string
	ActiveTurnID         string
	WaitingOnApproval    bool
	WaitingOnUserInput   bool
}

type claudeSessionMeta struct {
	ID                   string
	Title                string
	Preview              string
	WorkspaceKey         string
	CWD                  string
	Model                string
	AccessMode           string
	PlanMode             string
	NativePermissionMode string
	UpdatedAt            time.Time
}

type SessionMeta = claudeSessionMeta

type claudeSessionEntry struct {
	Meta   claudeSessionMeta
	Thread agentproto.ThreadSnapshotRecord
}

func (s RuntimeStateSnapshot) currentRuntimeStatus() *agentproto.ThreadRuntimeStatus {
	if strings.TrimSpace(s.SessionID) == "" {
		return nil
	}
	status := &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeIdle}
	if strings.TrimSpace(s.ActiveTurnID) != "" || s.WaitingOnApproval || s.WaitingOnUserInput {
		status.Type = agentproto.ThreadRuntimeStatusTypeActive
	}
	if s.WaitingOnApproval {
		status.ActiveFlags = append(status.ActiveFlags, agentproto.ThreadActiveFlagWaitingOnApproval)
	}
	if s.WaitingOnUserInput {
		status.ActiveFlags = append(status.ActiveFlags, agentproto.ThreadActiveFlagWaitingOnUserInput)
	}
	return status
}

func HandleLocalCommand(command agentproto.Command, workspaceRoot string, runtime RuntimeStateSnapshot) ([]agentproto.Event, bool, error) {
	switch command.Kind {
	case agentproto.CommandThreadsRefresh:
		threads, err := listSessionThreads(workspaceRoot, false, runtime)
		if err != nil {
			return nil, true, localSessionPlaneError(command, "claude_threads_refresh_failed", "wrapper 无法读取 Claude 本地会话目录。", err)
		}
		return []agentproto.Event{{
			Kind:      agentproto.EventThreadsSnapshot,
			CommandID: strings.TrimSpace(command.CommandID),
			ThreadID:  strings.TrimSpace(runtime.SessionID),
			Threads:   threads,
		}}, true, nil
	case agentproto.CommandThreadHistoryRead:
		history, err := readThreadHistory(workspaceRoot, command.Target.ThreadID, runtime)
		if err != nil {
			return nil, true, localSessionPlaneError(command, "claude_thread_history_read_failed", "wrapper 无法读取 Claude 本地会话历史。", err)
		}
		return []agentproto.Event{{
			Kind:          agentproto.EventThreadHistoryRead,
			CommandID:     strings.TrimSpace(command.CommandID),
			ThreadID:      strings.TrimSpace(command.Target.ThreadID),
			ThreadHistory: history,
		}}, true, nil
	default:
		return nil, false, nil
	}
}

func ListSessionMeta(workspaceRoot string, includeAll bool) ([]SessionMeta, error) {
	dirs, strictWorkspaceFilter, err := sessionProjectDirs(workspaceRoot, includeAll)
	if err != nil {
		return nil, err
	}
	entries, err := scanSessionEntries(dirs, workspaceRoot, strictWorkspaceFilter, RuntimeStateSnapshot{})
	if err != nil {
		return nil, err
	}
	metas := make([]SessionMeta, 0, len(entries))
	for _, entry := range entries {
		metas = append(metas, entry.Meta)
	}
	return metas, nil
}

func listSessionThreads(workspaceRoot string, includeAll bool, runtime RuntimeStateSnapshot) ([]agentproto.ThreadSnapshotRecord, error) {
	dirs, strictWorkspaceFilter, err := sessionProjectDirs(workspaceRoot, includeAll)
	if err != nil {
		return nil, err
	}
	entries, err := scanSessionEntries(dirs, workspaceRoot, strictWorkspaceFilter, runtime)
	if err != nil {
		return nil, err
	}
	threads := make([]agentproto.ThreadSnapshotRecord, 0, len(entries))
	for index, entry := range entries {
		thread := entry.Thread
		thread.ListOrder = index + 1
		threads = append(threads, thread)
	}
	return threads, nil
}

func scanSessionEntries(dirs []string, workspaceRoot string, strictWorkspaceFilter bool, runtime RuntimeStateSnapshot) ([]claudeSessionEntry, error) {
	seen := map[string]claudeSessionEntry{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
				continue
			}
			filePath := filepath.Join(dir, entry.Name())
			meta, err := readSessionListMeta(filePath)
			if err != nil || strings.TrimSpace(meta.ID) == "" {
				continue
			}
			if strictWorkspaceFilter && !sameWorkspaceCWD(meta.WorkspaceKey, workspaceRoot) {
				continue
			}
			record := claudeSessionEntry{
				Meta:   meta,
				Thread: buildSessionThreadSnapshot(meta, runtime),
			}
			current, ok := seen[meta.ID]
			if !ok || record.Meta.UpdatedAt.After(current.Meta.UpdatedAt) {
				seen[meta.ID] = record
			}
		}
	}
	ordered := make([]claudeSessionEntry, 0, len(seen))
	for _, entry := range seen {
		ordered = append(ordered, entry)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Meta.UpdatedAt.Equal(ordered[j].Meta.UpdatedAt) {
			return ordered[i].Meta.ID < ordered[j].Meta.ID
		}
		return ordered[i].Meta.UpdatedAt.After(ordered[j].Meta.UpdatedAt)
	})
	return ordered, nil
}

func buildSessionThreadSnapshot(meta claudeSessionMeta, runtime RuntimeStateSnapshot) agentproto.ThreadSnapshotRecord {
	threadID := strings.TrimSpace(meta.ID)
	runtimeStatus := &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeNotLoaded}
	loaded := false
	state := string(agentproto.ThreadRuntimeStatusTypeNotLoaded)
	model := strings.TrimSpace(meta.Model)
	accessMode := strings.TrimSpace(meta.AccessMode)
	planMode := strings.TrimSpace(meta.PlanMode)
	if threadID != "" && threadID == strings.TrimSpace(runtime.SessionID) {
		if current := runtime.currentRuntimeStatus(); current != nil {
			runtimeStatus = current
			loaded = current.IsLoaded()
			state = string(current.LegacyState())
		}
		model = firstNonEmptyString(strings.TrimSpace(runtime.Model), model)
		accessMode = firstNonEmptyString(strings.TrimSpace(runtime.AccessMode), accessMode)
		planMode = firstNonEmptyString(strings.TrimSpace(runtime.PlanMode), planMode)
	}
	return agentproto.ThreadSnapshotRecord{
		ThreadID:      threadID,
		Name:          strings.TrimSpace(meta.Title),
		Preview:       strings.TrimSpace(meta.Preview),
		WorkspaceKey:  strings.TrimSpace(meta.WorkspaceKey),
		CWD:           strings.TrimSpace(meta.CWD),
		Model:         model,
		AccessMode:    accessMode,
		PlanMode:      planMode,
		Loaded:        loaded,
		Archived:      false,
		State:         state,
		RuntimeStatus: agentproto.CloneThreadRuntimeStatus(runtimeStatus),
	}
}

func sessionProjectDirs(workspaceRoot string, includeAll bool) ([]string, bool, error) {
	projectsDir := claudeProjectsDir()
	if projectsDir == "" {
		return nil, false, nil
	}
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	all := make([]string, 0, len(entries))
	specific := ""
	if strings.TrimSpace(workspaceRoot) != "" {
		specific = filepath.Join(projectsDir, SanitizeProjectDirName(workspaceRoot))
	}
	specificFound := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, entry.Name())
		all = append(all, dir)
		if specific != "" && dir == specific {
			specificFound = true
		}
	}
	if includeAll || strings.TrimSpace(workspaceRoot) == "" {
		return all, false, nil
	}
	if specificFound {
		return []string{specific}, false, nil
	}
	return all, true, nil
}

func claudeProjectsDir() string {
	configDir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if configDir == "" {
		home := claudeHomeDir()
		if home == "" {
			return ""
		}
		configDir = filepath.Join(home, ".claude")
	}
	return filepath.Join(configDir, "projects")
}

func SanitizeProjectDirName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	out := builder.String()
	if len(out) <= claudeProjectDirNameMax {
		return out
	}
	return out[:claudeProjectDirNameMax] + "-" + simpleProjectPathHash(value)
}

func simpleProjectPathHash(value string) string {
	hash := 0
	for _, r := range value {
		hash = ((hash << 5) - hash) + int(r)
	}
	if hash < 0 {
		hash = -hash
	}
	return fmt.Sprintf("%x", hash)
}

func readSessionListMeta(filePath string) (claudeSessionMeta, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return claudeSessionMeta{}, err
	}
	head, tail, err := readSessionHeadTail(filePath, stat.Size())
	if err != nil {
		return claudeSessionMeta{}, err
	}
	meta := claudeSessionMeta{
		ID:        strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)),
		UpdatedAt: stat.ModTime(),
	}
	parseSessionChunkReverse(tail, func(entry map[string]any) bool {
		populateSessionTailMeta(&meta, entry)
		return sessionTailMetaComplete(meta)
	})
	parseSessionChunkForward(head, func(entry map[string]any) bool {
		populateSessionHeadMeta(&meta, entry)
		return sessionHeadMetaComplete(meta)
	})
	meta.WorkspaceKey = firstNonEmptyString(meta.WorkspaceKey, meta.CWD)
	selection := claudePermissionSelectionFromNative(meta.NativePermissionMode)
	meta.AccessMode = selection.AccessMode
	meta.PlanMode = selection.PlanMode
	meta.Title = normalizeSessionSnippet(firstNonEmptyString(meta.Title, meta.Preview, meta.ID), claudeSessionTitleLimit)
	meta.Preview = normalizeSessionSnippet(meta.Preview, claudeSessionPreviewLimit)
	if meta.Preview == meta.Title {
		meta.Preview = ""
	}
	return meta, nil
}

func populateSessionTailMeta(meta *claudeSessionMeta, entry map[string]any) {
	if meta == nil {
		return
	}
	if meta.CWD == "" {
		meta.CWD = strings.TrimSpace(lookupStringFromAny(entry["cwd"]))
	}
	if meta.Title == "" {
		meta.Title = sessionEntryTitle(entry)
	}
	if meta.Preview == "" {
		meta.Preview = sessionEntryPreview(entry)
	}
	if meta.Model == "" {
		meta.Model = strings.TrimSpace(lookupStringFromAny(entry["model"]))
	}
	if meta.NativePermissionMode == "" {
		meta.NativePermissionMode = strings.TrimSpace(lookupStringFromAny(entry["permissionMode"]))
	}
}

func populateSessionHeadMeta(meta *claudeSessionMeta, entry map[string]any) {
	if meta == nil {
		return
	}
	if meta.WorkspaceKey == "" {
		meta.WorkspaceKey = strings.TrimSpace(lookupStringFromAny(entry["cwd"]))
	}
	if meta.Title == "" {
		meta.Title = sessionEntryTitle(entry)
	}
	if meta.Preview == "" {
		meta.Preview = sessionEntryPreview(entry)
	}
	if meta.Model == "" {
		meta.Model = strings.TrimSpace(lookupStringFromAny(entry["model"]))
	}
	if meta.NativePermissionMode == "" {
		meta.NativePermissionMode = strings.TrimSpace(lookupStringFromAny(entry["permissionMode"]))
	}
}

func sessionTailMetaComplete(meta claudeSessionMeta) bool {
	return meta.CWD != "" && meta.Title != "" && meta.Preview != ""
}

func sessionHeadMetaComplete(meta claudeSessionMeta) bool {
	return meta.WorkspaceKey != "" && meta.Title != "" && meta.Preview != ""
}

func readSessionHeadTail(filePath string, size int64) (string, string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	headSize := size
	if headSize > claudeSessionMetaReadBytes {
		headSize = claudeSessionMetaReadBytes
	}
	head := make([]byte, headSize)
	if headSize > 0 {
		n, err := file.Read(head)
		if err != nil {
			return "", "", err
		}
		head = head[:n]
	}

	tailSize := size
	if tailSize > claudeSessionMetaReadBytes {
		tailSize = claudeSessionMetaReadBytes
	}
	tail := make([]byte, tailSize)
	if tailSize > 0 {
		n, err := file.ReadAt(tail, size-tailSize)
		if err != nil && n == 0 {
			return "", "", err
		}
		tail = tail[:n]
	}
	return string(head), string(tail), nil
}

func parseSessionChunkForward(chunk string, fn func(map[string]any) bool) {
	lines := strings.Split(chunk, "\n")
	for _, line := range lines {
		entry, ok := parseSessionLine(line)
		if !ok {
			continue
		}
		if fn(entry) {
			return
		}
	}
}

func parseSessionChunkReverse(chunk string, fn func(map[string]any) bool) {
	lines := strings.Split(chunk, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		entry, ok := parseSessionLine(lines[index])
		if !ok {
			continue
		}
		if fn(entry) {
			return
		}
	}
}

func parseSessionLine(line string) (map[string]any, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return nil, false
	}
	return entry, true
}

func sessionEntryTitle(entry map[string]any) string {
	switch strings.TrimSpace(lookupStringFromAny(entry["type"])) {
	case "custom-title":
		return normalizeSessionSnippet(lookupStringFromAny(entry["customTitle"]), claudeSessionTitleLimit)
	case "session-title":
		return normalizeSessionSnippet(firstNonEmptyString(lookupStringFromAny(entry["title"]), lookupStringFromAny(entry["name"])), claudeSessionTitleLimit)
	case "ai-title":
		return normalizeSessionSnippet(lookupStringFromAny(entry["aiTitle"]), claudeSessionTitleLimit)
	case "summary":
		return normalizeSessionSnippet(lookupStringFromAny(entry["summary"]), claudeSessionTitleLimit)
	default:
		return ""
	}
}

func sessionEntryPreview(entry map[string]any) string {
	switch strings.TrimSpace(lookupStringFromAny(entry["type"])) {
	case "last-prompt":
		return normalizeSessionSnippet(lookupStringFromAny(entry["lastPrompt"]), claudeSessionPreviewLimit)
	case "user":
		message, _ := entry["message"].(map[string]any)
		return normalizeSessionSnippet(sessionMessageText(message["content"]), claudeSessionPreviewLimit)
	default:
		return ""
	}
}

func sessionMessageText(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, blockValue := range value {
			block, _ := blockValue.(map[string]any)
			if strings.TrimSpace(lookupStringFromAny(block["type"])) != "text" {
				continue
			}
			text := strings.TrimSpace(lookupStringFromAny(block["text"]))
			if text == "" {
				continue
			}
			parts = append(parts, text)
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

func normalizeSessionSnippet(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	if limit > 0 && len(value) > limit {
		value = strings.TrimSpace(value[:limit]) + "..."
	}
	return value
}

func sameWorkspaceCWD(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
