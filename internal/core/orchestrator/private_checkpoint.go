package orchestrator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// PrivateCheckpoint is a versioned projection, never a claim that an old
// process or turn is alive. All pending bindings remain held for reconciliation.
type PrivateCheckpoint struct {
	Version                                                                                         int `json:"version"`
	SurfaceID, GatewayID, ChatID, ActorUserID                                                       string
	Backend                                                                                         agentproto.Backend
	CodexProfileID, ClaudeProfileID, OpenCodeProfileID                                              string
	CodexAdmissionRef                                                                               *state.CodexAdmissionRef
	CodexConnectionContract                                                                         *state.CodexConnectionContract
	CodexThreadPolicy                                                                               *state.CodexThreadPolicy
	ContractRefreshPending                                                                          bool `json:",omitempty"`
	OpenCodeAdmissionRef                                                                            *state.OpenCodeAdmissionRef
	Verbosity                                                                                       state.SurfaceVerbosity
	PlanMode                                                                                        state.PlanModeSetting
	PlanModeOverrideSet                                                                             bool
	PromptOverride                                                                                  state.ModelConfigRecord
	AutoWhip                                                                                        state.AutoWhipRuntimeRecord
	AutoContinue                                                                                    state.AutoContinueRuntimeRecord
	RouteMode                                                                                       state.RouteMode
	ClaimedWorkspaceKey, AttachedInstanceID, SelectedThreadID                                       string
	PreparedThreadCWD, PreparedFromThreadID                                                         string
	PreparedAt                                                                                      time.Time
	ActiveQueueItemID                                                                               string
	QueuedQueueItemIDs                                                                              []string
	QueueItems                                                                                      map[string]*state.QueueItemRecord
	StagedImages                                                                                    map[string]*state.StagedImageRecord
	StagedFiles                                                                                     map[string]*state.StagedFileRecord
	SurfaceMessages                                                                                 map[string]*state.SurfaceMessageRecord `json:",omitempty"`
	SurfaceMessageSeq                                                                               int                                    `json:",omitempty"`
	RecentFinalCards                                                                                []*state.FinalCardRecord               `json:",omitempty"`
	HistoricalInstance                                                                              *PrivateHistoricalInstance
	Holds                                                                                           []PrivateRecoveryHold
	Goal                                                                                            *PrivateGoalCheckpoint     `json:",omitempty"`
	DetachedExecutions                                                                              []PrivateDetachedExecution `json:",omitempty"`
	OfflineStop                                                                                     *PrivateOfflineStop        `json:",omitempty"`
	Navigation                                                                                      *PrivateNavigationSnapshot `json:",omitempty"`
	ReconnectQuery                                                                                  *PrivateReconnectQuery     `json:",omitempty"`
	PickerWatermarks                                                                                *PrivatePickerWatermarks
	MigrationRecoveryReleased                                                                       bool                 `json:",omitempty"`
	UI                                                                                              *privateCheckpointUI `json:",omitempty"`
	NextQueueID, NextImageID, NextFileID, NextCommandID, NextHeadlessID, NextEpisodeID, NextShellID int
}

type PrivateReconnectQuery struct {
	InstanceID, ThreadID, CommandID string
	Deadline                        time.Time
}

type PrivateOfflineStop struct{ InstanceID, ThreadID, TurnID, SourceMessageID, CommandID, QueueItemID string }

// PrivateDetachedExecution keeps immutable references to old execution evidence.
// Its queue IDs refer to QueueItems; it is never a dispatch queue.
type PrivateDetachedExecution struct {
	InstanceID         string
	ActiveQueueItemID  string
	QueuedQueueItemIDs []string
	Holds              []PrivateRecoveryHold
	Events             []PrivateDetachedEventRef `json:",omitempty"`
	OfflineStop        *PrivateOfflineStop       `json:",omitempty"`
}
type PrivateDetachedEventRef struct {
	Incarnation string
	Sequence    uint64
	PayloadHash string
}
type PrivateHistoricalInstance struct {
	InstanceID, WorkspaceRoot, WorkspaceKey string
	Backend                                 agentproto.Backend
}
type PrivateRecoveryHold struct {
	Kind, InstanceID, CommandID, QueueItemID, ThreadID, TurnID string
	Frozen                                                     json.RawMessage
}

// ExportPrivateCheckpoint captures only private business state, with pending
// native work as opaque held evidence rather than an active connection.
func (s *Service) ExportPrivateCheckpoint(surfaceID string) (PrivateCheckpoint, error) {
	var out PrivateCheckpoint
	if !s.config.PrivateApplication {
		return out, fmt.Errorf("private service required")
	}
	v := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if v == nil || surfaceFeishuRoomID(v) != "" || state.IsVSCodeProductMode(v.ProductMode) || v.RouteMode == state.RouteModeFollowLocal {
		return out, fmt.Errorf("private surface required")
	}
	out = PrivateCheckpoint{Version: 1, SurfaceID: v.SurfaceSessionID, GatewayID: v.GatewayID, ChatID: v.ChatID, ActorUserID: v.ActorUserID, Backend: v.Backend, CodexProfileID: v.CodexProfileID, ClaudeProfileID: v.ClaudeProfileID, OpenCodeProfileID: v.OpenCodeProfileID, CodexAdmissionRef: v.CodexAdmissionRef, CodexConnectionContract: v.CodexConnectionContract, CodexThreadPolicy: v.CodexThreadPolicy, ContractRefreshPending: v.ContractRefreshPending, OpenCodeAdmissionRef: v.OpenCodeAdmissionRef, Verbosity: v.Verbosity, PlanMode: v.PlanMode, PlanModeOverrideSet: v.PlanModeOverrideSet, PromptOverride: v.PromptOverride, AutoWhip: v.AutoWhip, AutoContinue: v.AutoContinue, RouteMode: v.RouteMode, ClaimedWorkspaceKey: v.ClaimedWorkspaceKey, AttachedInstanceID: v.AttachedInstanceID, SelectedThreadID: v.SelectedThreadID, PreparedThreadCWD: v.PreparedThreadCWD, PreparedFromThreadID: v.PreparedFromThreadID, PreparedAt: v.PreparedAt, ActiveQueueItemID: v.ActiveQueueItemID, QueuedQueueItemIDs: v.QueuedQueueItemIDs, QueueItems: v.QueueItems, StagedImages: v.StagedImages, StagedFiles: v.StagedFiles, NextQueueID: s.nextQueueItemID, NextImageID: s.nextImageID, NextFileID: s.nextFileID, NextCommandID: s.nextRequestCommandID, NextHeadlessID: s.nextHeadlessID, NextEpisodeID: s.nextAutoContinueEpisodeID, NextShellID: s.nextShellBindingSequence}
	pickers := s.privatePickerWatermarks()
	out.PickerWatermarks = &pickers
	out.SurfaceMessages, out.SurfaceMessageSeq, out.RecentFinalCards = v.SurfaceMessages, v.SurfaceMessageSeq, v.RecentFinalCards
	out.UI = s.exportPrivateCheckpointUI(v)
	out.MigrationRecoveryReleased = s.privateMigrationReleased[v.SurfaceSessionID]
	out.Goal = s.exportPrivateGoalCheckpoint(v.SurfaceSessionID)
	out.Navigation = s.exportPrivateNavigation(v)
	out.DetachedExecutions = append([]PrivateDetachedExecution(nil), s.privateDetachedExecutions[v.SurfaceSessionID]...)
	if stop, ok := s.privateOfflineStops[v.SurfaceSessionID]; ok {
		out.OfflineStop = &stop
	}
	if query, ok := s.privateReconnectQueries[v.SurfaceSessionID]; ok {
		if v.RouteMode == state.RouteModePinned && v.AttachedInstanceID == query.InstanceID && v.SelectedThreadID == query.ThreadID {
			out.ReconnectQuery = &query
		} else {
			delete(s.privateReconnectQueries, v.SurfaceSessionID)
		}
	}
	if len(out.DetachedExecutions) > 0 || out.OfflineStop != nil || out.ReconnectQuery != nil || out.Goal != nil || out.Navigation != nil {
		out.Version = 2
	}

	if inst := s.root.Instances[v.AttachedInstanceID]; inst != nil {
		out.HistoricalInstance = &PrivateHistoricalInstance{inst.InstanceID, inst.WorkspaceRoot, inst.WorkspaceKey, inst.Backend}
	}
	add := func(kind, instance, command, item, thread, turn string, value any) {
		raw, _ := json.Marshal(value)
		out.Holds = append(out.Holds, PrivateRecoveryHold{kind, instance, command, item, thread, turn, raw})
	}
	if held, ok := s.privateRecoveryHolds[v.SurfaceSessionID]; ok {
		out.Holds = append(out.Holds, held.Holds...)
		if out.HistoricalInstance == nil {
			out.HistoricalInstance = held.HistoricalInstance
		}
	}
	s.turns.forEachPendingRemote(func(b *remoteTurnBinding) {
		if b.SurfaceSessionID == v.SurfaceSessionID {
			add("pending_remote", b.InstanceID, b.CommandID, b.QueueItemID, b.ThreadID, b.TurnID, b)
		}
	})
	s.turns.forEachActiveRemote(func(b *remoteTurnBinding) {
		if b.SurfaceSessionID == v.SurfaceSessionID {
			add("active_remote", b.InstanceID, b.CommandID, b.QueueItemID, b.ThreadID, b.TurnID, b)
		}
	})
	s.turns.forEachPendingSteer(func(_ string, b *pendingSteerBinding) {
		if b.SurfaceSessionID == v.SurfaceSessionID {
			add("pending_steer", b.InstanceID, b.CommandID, b.QueueItemID, b.ThreadID, b.TurnID, b)
		}
	})
	s.turns.forEachPendingShell(func(_ string, b *pendingShellBinding) {
		if b.SurfaceSessionID == v.SurfaceSessionID {
			add("pending_shell", b.InstanceID, b.CommandID, b.QueueItemID, b.ThreadID, b.TurnID, b)
		}
	})
	s.turns.forEachPrivateCompact(v.SurfaceSessionID, func(b *compactTurnBinding) { add("compact", b.InstanceID, b.CommandID, "", b.ThreadID, b.TurnID, b) })
	s.turns.forEachPrivatePendingReview(v.SurfaceSessionID, func(command string) { add("pending_review", v.AttachedInstanceID, command, "", "", "", command) })
	if v.PendingHeadless != nil {
		add("headless_launch", v.AttachedInstanceID, "", "", "", "", v.PendingHeadless)
	}
	if len(v.PendingRequests) > 0 || v.ActiveRequestCapture != nil {
		add("requests", v.AttachedInstanceID, "", "", "", "", struct {
			Requests map[string]*state.RequestPromptRecord
			Order    []string
			Capture  *state.RequestCaptureRecord
		}{v.PendingRequests, v.PendingRequestOrder, v.ActiveRequestCapture})
	}
	if v.PendingTextInput != nil {
		add("pending_text", v.AttachedInstanceID, "", "", "", "", v.PendingTextInput)
	}
	if v.ReviewSession != nil {
		add("review_session", v.AttachedInstanceID, "", "", "", "", v.ReviewSession)
	}
	if v.ActiveExecProgress != nil {
		add("exec_progress", v.AttachedInstanceID, "", "", "", "", v.ActiveExecProgress)
	}
	if v.ActiveReasoning != nil {
		add("reasoning_progress", v.AttachedInstanceID, "", "", "", "", v.ActiveReasoning)
	}
	if v.Abandoning {
		add("abandoning", v.AttachedInstanceID, "", v.ActiveQueueItemID, v.SelectedThreadID, "", struct{ Deadline time.Time }{s.abandoningUntil[v.SurfaceSessionID]})
	}
	s.forEachItemBuffer(func(buffer *itemBuffer) {
		if buffer.InstanceID != "" && buffer.InstanceID == v.AttachedInstanceID {
			add("item_buffer", buffer.InstanceID, "", "", buffer.ThreadID, buffer.TurnID, struct {
				ItemID, ItemKind, TextValue string
				TextChunks                  []string
			}{buffer.ItemID, buffer.ItemKind, buffer.textValue, buffer.textChunks})
		}
	})
	s.progress.forEachPrivateText(v.AttachedInstanceID, func(kind string, item *completedTextItem) {
		add(kind, item.InstanceID, "", "", item.ThreadID, item.TurnID, item)
	})
	s.progress.forEachPrivateAggregate(v.AttachedInstanceID, func(kind, thread, turn string, value any) {
		add(kind, v.AttachedInstanceID, "", "", thread, turn, value)
	})
	sort.Slice(out.Holds, func(i, j int) bool {
		a, b := out.Holds[i], out.Holds[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.InstanceID != b.InstanceID {
			return a.InstanceID < b.InstanceID
		}
		if a.CommandID != b.CommandID {
			return a.CommandID < b.CommandID
		}
		return a.QueueItemID < b.QueueItemID
	})
	// Copy through the wire format so callers cannot mutate live state.
	raw, err := json.Marshal(out)
	if err != nil {
		return PrivateCheckpoint{}, err
	}
	var copy PrivateCheckpoint
	if err = json.Unmarshal(raw, &copy); err != nil {
		return PrivateCheckpoint{}, err
	}
	return copy, nil
}
