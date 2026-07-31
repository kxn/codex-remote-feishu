package daemon

import (
	"log"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishuroomstate"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

type feishuRoomWorkspaceCandidate struct {
	workspaceKey    string
	updatedBy       string
	updatedAt       time.Time
	sourceSurfaceID string
}

func (a *App) configureFeishuRoomStateLocked(stateDir string) {
	path := feishuroomstate.StatePath(stateDir)
	a.feishuRoomState.persistedStoreRuntimeState = loadPersistedStore("Feishu room", path, feishuroomstate.LoadStore)
	a.materializeFeishuRoomStateLocked()
}

func (a *App) materializeFeishuRoomStateLocked() {
	if a.feishuRoomState.store == nil {
		return
	}
	entries := a.feishuRoomState.store.Entries()
	records := make([]state.FeishuRoomStateRecord, 0, len(entries))
	for _, record := range entries {
		records = append(records, record)
	}
	a.service.MaterializeFeishuRoomState(records)
	a.refreshFeishuPrimaryGatewaySnapshotLocked()
}

func (a *App) syncFeishuRoomStateLocked() {
	a.refreshFeishuPrimaryGatewaySnapshotLocked()
	if !a.feishuRoomState.writable() || a.feishuRoomState.store == nil {
		return
	}
	if err := a.feishuRoomState.store.ReplaceAll(a.service.FeishuRoomState()); err != nil {
		log.Printf("persist feishu room state failed: err=%v", err)
	}
}

func (a *App) refreshFeishuPrimaryGatewaySnapshotLocked() {
	if a == nil {
		return
	}
	a.feishuRuntime.primaryGatewayByChat.Store(buildFeishuPrimaryGatewayIndex(a.service.FeishuRoomState()))
}

func buildFeishuPrimaryGatewayIndex(records []state.FeishuRoomStateRecord) map[string]string {
	index := map[string]string{}
	for _, record := range records {
		normalized, ok := state.NormalizeFeishuRoomStateRecord(record)
		if !ok {
			continue
		}
		primary := canonicalGatewayID(normalized.PrimaryGatewayID)
		if primary == "" {
			continue
		}
		if key := state.FeishuRoomKey(normalized.ChatID); key != "" {
			index[key] = primary
		}
	}
	return index
}

func (a *App) feishuPrimaryGatewayForChat(chatID string) string {
	if a == nil {
		return ""
	}
	key := state.FeishuRoomKey(chatID)
	if key == "" {
		return ""
	}
	snapshot, _ := a.feishuRuntime.primaryGatewayByChat.Load().(map[string]string)
	if len(snapshot) == 0 {
		return ""
	}
	return snapshot[key]
}

func (a *App) reconcileFeishuRoomWorkspaceStateLocked(entries map[string]surfaceresume.Entry) {
	a.feishuRoomState.workspaceConflicts = map[string]bool{}
	if a.feishuRoomState.store == nil || len(entries) == 0 {
		return
	}
	candidates := map[string]map[string]feishuRoomWorkspaceCandidate{}
	for _, entry := range entries {
		ref, ok := feishuidentity.ParseSurfaceRef(entry.SurfaceSessionID)
		if !ok || !ref.IsChat() {
			continue
		}
		workspaceKey := state.ResolveHeadlessResumeWorkspaceKey(entry.ResumeWorkspaceKey, entry.ResumeThreadCWD)
		roomID := state.FeishuRoomKey(ref.ScopeID)
		if workspaceKey == "" || roomID == "" {
			continue
		}
		if candidates[roomID] == nil {
			candidates[roomID] = map[string]feishuRoomWorkspaceCandidate{}
		}
		candidate, exists := candidates[roomID][workspaceKey]
		if !exists || entry.UpdatedAt.After(candidate.updatedAt) ||
			entry.UpdatedAt.Equal(candidate.updatedAt) && entry.SurfaceSessionID < candidate.sourceSurfaceID {
			candidates[roomID][workspaceKey] = feishuRoomWorkspaceCandidate{
				workspaceKey:    workspaceKey,
				updatedBy:       strings.TrimSpace(entry.ActorUserID),
				updatedAt:       entry.UpdatedAt,
				sourceSurfaceID: entry.SurfaceSessionID,
			}
		}
	}
	for roomID, roomCandidates := range candidates {
		record, _ := a.feishuRoomState.store.Get(roomID)
		if record.WorkspaceKey != "" {
			if len(roomCandidates) != 1 {
				a.recordFeishuRoomWorkspaceConflictLocked(roomID, roomCandidates)
				continue
			}
			if _, matches := roomCandidates[state.ResolveWorkspaceClaimKey(record.WorkspaceKey)]; !matches {
				a.recordFeishuRoomWorkspaceConflictLocked(roomID, roomCandidates)
			}
			continue
		}
		if len(roomCandidates) != 1 {
			a.recordFeishuRoomWorkspaceConflictLocked(roomID, roomCandidates)
			continue
		}
		if !a.feishuRoomState.writable() {
			continue
		}
		for _, candidate := range roomCandidates {
			record.RoomID = roomID
			record.ChatID = strings.TrimPrefix(roomID, "feishu:chat:")
			record.WorkspaceKey = candidate.workspaceKey
			record.WorkspaceUpdatedBy = candidate.updatedBy
			record.WorkspaceUpdatedAt = candidate.updatedAt
			if err := a.feishuRoomState.store.Put(record); err != nil {
				log.Printf("backfill feishu room workspace state failed: room=%s err=%v", roomID, err)
			}
		}
	}
}

func (a *App) recordFeishuRoomWorkspaceConflictLocked(roomID string, candidates map[string]feishuRoomWorkspaceCandidate) {
	workspaceKeys := make([]string, 0, len(candidates))
	for workspaceKey := range candidates {
		workspaceKeys = append(workspaceKeys, workspaceKey)
	}
	sort.Strings(workspaceKeys)
	a.feishuRoomState.workspaceConflicts[roomID] = true
	log.Printf("feishu room workspace recovery conflict: room=%s workspaces=%q", roomID, workspaceKeys)
}

func (a *App) feishuRoomWorkspaceConflictNotice(action control.Action) *control.Notice {
	ref, ok := feishuidentity.ParseSurfaceRef(action.SurfaceSessionID)
	if !ok || !ref.IsChat() {
		return nil
	}
	roomID := state.FeishuRoomKey(ref.ScopeID)
	_, ok = a.feishuRoomState.workspaceConflicts[roomID]
	if !ok {
		return nil
	}
	return &control.Notice{
		Code:     "room_workspace_recovery_conflict",
		Title:    "群工作区状态冲突",
		Text:     "检测到这个群保存了多个不一致的工作区，已停止恢复以避免进入错误目录。请检查 daemon 日志并修复持久化状态后重启。",
		ThemeKey: "error",
	}
}
