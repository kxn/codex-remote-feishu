package orchestrator

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// RestorePrivateCheckpoint keeps all input state frozen. It does not recreate
// turn bindings or register the historical instance as online. The host must
// reconcile the holds with journal and native evidence before enabling work.
func (s *Service) RestorePrivateCheckpoint(c PrivateCheckpoint) error {
	if !s.config.PrivateApplication || (c.Version != 1 && c.Version != 2) || (c.Version == 1 && (len(c.DetachedExecutions) != 0 || c.OfflineStop != nil || c.ReconnectQuery != nil || c.Goal != nil || c.Navigation != nil)) || strings.TrimSpace(c.SurfaceID) == "" || strings.TrimSpace(c.GatewayID) == "" || strings.TrimSpace(c.ChatID) == "" || strings.TrimSpace(c.ActorUserID) == "" || c.RouteMode == state.RouteModeFollowLocal {
		return fmt.Errorf("invalid private checkpoint")
	}
	if _, err := c.IDWatermarks(); err != nil {
		return err
	}
	if _, ok := s.privateRecoveryHolds[c.SurfaceID]; ok {
		return fmt.Errorf("surface already held")
	}
	if old := s.root.Surfaces[c.SurfaceID]; old != nil && (len(old.QueueItems) != 0 || len(old.StagedImages) != 0 || len(old.StagedFiles) != 0 || old.AttachedInstanceID != "" || old.SelectedThreadID != "") {
		return fmt.Errorf("surface already active")
	}
	if old := s.root.Surfaces[c.SurfaceID]; old != nil && (old.GatewayID != c.GatewayID || old.ChatID != c.ChatID || old.ActorUserID != c.ActorUserID) {
		return fmt.Errorf("surface owner mismatch")
	}
	if c.ReconnectQuery != nil && (c.ReconnectQuery.InstanceID == "" || c.ReconnectQuery.InstanceID != c.AttachedInstanceID || c.ReconnectQuery.ThreadID == "" || c.ReconnectQuery.ThreadID != c.SelectedThreadID || c.ReconnectQuery.CommandID == "" || c.ReconnectQuery.Deadline.IsZero() || c.RouteMode != state.RouteModePinned) {
		return fmt.Errorf("invalid private reconnect query")
	}
	if c.HistoricalInstance != nil && (c.HistoricalInstance.InstanceID == "" || c.HistoricalInstance.InstanceID != c.AttachedInstanceID) {
		return fmt.Errorf("historical instance mismatch")
	}
	if err := validatePrivateCheckpointUI(c.UI, c.ActorUserID); err != nil {
		return err
	}
	if err := s.validatePrivateGoalCheckpoint(c.SurfaceID, c.Goal); err != nil {
		return err
	}
	for _, h := range c.Holds {
		if h.Kind == "" || !json.Valid(h.Frozen) {
			return fmt.Errorf("invalid recovery hold")
		}
	}
	if c.OfflineStop != nil && (c.OfflineStop.InstanceID == "" || (c.AttachedInstanceID != "" && c.OfflineStop.InstanceID != c.AttachedInstanceID) || (c.OfflineStop.TurnID != "" && c.OfflineStop.ThreadID == "")) {
		return fmt.Errorf("invalid offline stop")
	}
	detachedInstances := map[string]bool{}
	detachedInputs := map[string]bool{}
	for _, detached := range c.DetachedExecutions {
		if detached.InstanceID == "" || detachedInstances[detached.InstanceID] || detached.InstanceID == c.AttachedInstanceID {
			return fmt.Errorf("invalid detached execution instance")
		}
		detachedInstances[detached.InstanceID] = true
		if c.OfflineStop != nil && c.OfflineStop.InstanceID == detached.InstanceID {
			return fmt.Errorf("offline stop targets detached execution")
		}
		if detached.OfflineStop != nil && detached.OfflineStop.InstanceID != detached.InstanceID {
			return fmt.Errorf("invalid detached offline stop")
		}

		ids := append([]string(nil), detached.QueuedQueueItemIDs...)
		if detached.ActiveQueueItemID != "" {
			ids = append(ids, detached.ActiveQueueItemID)
		}
		for _, id := range ids {
			if id == "" || c.QueueItems[id] == nil || detachedInputs[id] || id == c.ActiveQueueItemID {
				return fmt.Errorf("invalid detached execution input")
			}
			detachedInputs[id] = true
		}
		eventRefs := map[string]bool{}
		for _, event := range detached.Events {
			if event.Incarnation == "" || event.Sequence == 0 || event.PayloadHash == "" {
				return fmt.Errorf("invalid detached event reference")
			}
			key := fmt.Sprintf("%s:%d", event.Incarnation, event.Sequence)
			if eventRefs[key] {
				return fmt.Errorf("duplicate detached event reference")
			}
			eventRefs[key] = true
		}
		for _, h := range detached.Holds {
			if h.Kind == "" || !json.Valid(h.Frozen) || (h.InstanceID != "" && h.InstanceID != detached.InstanceID) {
				return fmt.Errorf("invalid detached execution evidence")
			}
			if h.Kind == "headless_launch" {
				var launch state.HeadlessLaunchRecord
				if json.Unmarshal(h.Frozen, &launch) != nil || launch.InstanceID != detached.InstanceID {
					return fmt.Errorf("invalid detached launch evidence")
				}
			}
		}
	}
	for _, id := range c.QueuedQueueItemIDs {
		if detachedInputs[id] {
			return fmt.Errorf("detached input is queued")
		}
	}

	ids := map[string]bool{}
	for _, id := range c.QueuedQueueItemIDs {
		item := c.QueueItems[id]
		if id == "" || ids[id] || item == nil || item.ID != id || item.SurfaceSessionID != c.SurfaceID {
			return fmt.Errorf("invalid queued input")
		}
		ids[id] = true
	}
	if c.ActiveQueueItemID != "" {
		item := c.QueueItems[c.ActiveQueueItemID]
		if item == nil || item.ID != c.ActiveQueueItemID || item.SurfaceSessionID != c.SurfaceID || ids[c.ActiveQueueItemID] {
			return fmt.Errorf("invalid active input")
		}
	}
	for id, item := range c.QueueItems {
		if item == nil || item.ID != id || item.SurfaceSessionID != c.SurfaceID || !validCheckpointID(id, "queue-", c.NextQueueID) {
			return fmt.Errorf("invalid queue record")
		}
	}
	for id, item := range c.StagedImages {
		if item == nil || item.ImageID != id || item.SurfaceSessionID != c.SurfaceID || !validCheckpointID(id, "img-", c.NextImageID) {
			return fmt.Errorf("invalid image record")
		}
	}
	for id, item := range c.StagedFiles {
		if item == nil || item.FileID != id || item.SurfaceSessionID != c.SurfaceID || !validCheckpointID(id, "file-", c.NextFileID) {
			return fmt.Errorf("invalid file record")
		}
	}
	for id, message := range c.SurfaceMessages {
		if message == nil || id == "" || message.MessageID != id || message.AppendSeq < 1 || message.AppendSeq > c.SurfaceMessageSeq {
			return fmt.Errorf("invalid private surface message")
		}
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	var copy PrivateCheckpoint
	if err = json.Unmarshal(raw, &copy); err != nil {
		return err
	}
	s.ensureSurface(control.Action{SurfaceSessionID: copy.SurfaceID, GatewayID: copy.GatewayID, ChatID: copy.ChatID, ActorUserID: copy.ActorUserID})
	v := s.root.Surfaces[copy.SurfaceID]
	v.Backend = copy.Backend
	v.CodexProfileID = copy.CodexProfileID
	v.ClaudeProfileID = copy.ClaudeProfileID
	v.OpenCodeProfileID = copy.OpenCodeProfileID
	v.CodexAdmissionRef = copy.CodexAdmissionRef
	v.CodexConnectionContract = copy.CodexConnectionContract
	v.CodexThreadPolicy = copy.CodexThreadPolicy
	v.ContractRefreshPending = copy.ContractRefreshPending
	v.OpenCodeAdmissionRef = copy.OpenCodeAdmissionRef
	v.Verbosity = copy.Verbosity
	v.PlanMode = copy.PlanMode
	v.PlanModeOverrideSet = copy.PlanModeOverrideSet
	v.PromptOverride = copy.PromptOverride
	v.AutoWhip = copy.AutoWhip
	v.AutoContinue = copy.AutoContinue
	v.RouteMode = copy.RouteMode
	v.ClaimedWorkspaceKey = copy.ClaimedWorkspaceKey
	v.AttachedInstanceID = copy.AttachedInstanceID
	v.SelectedThreadID = copy.SelectedThreadID
	v.PreparedThreadCWD = copy.PreparedThreadCWD
	v.PreparedFromThreadID = copy.PreparedFromThreadID
	v.PreparedAt = copy.PreparedAt
	v.ActiveQueueItemID = copy.ActiveQueueItemID
	v.QueuedQueueItemIDs = copy.QueuedQueueItemIDs
	v.QueueItems = copy.QueueItems
	v.StagedImages = copy.StagedImages
	v.StagedFiles = copy.StagedFiles
	v.SurfaceMessages, v.SurfaceMessageSeq, v.RecentFinalCards = copy.SurfaceMessages, copy.SurfaceMessageSeq, copy.RecentFinalCards
	s.restorePrivateCheckpointUI(v, copy.UI)
	if err := s.restorePrivateGoalCheckpoint(copy.SurfaceID, copy.Goal); err != nil {
		return err
	}
	s.privateMigrationReleased[copy.SurfaceID] = copy.MigrationRecoveryReleased
	s.privateDetachedExecutions[copy.SurfaceID] = append([]PrivateDetachedExecution(nil), copy.DetachedExecutions...)
	if err := s.restorePrivateNavigation(copy); err != nil {
		return err
	}
	delete(s.privateReconnectQueries, copy.SurfaceID)
	if copy.ReconnectQuery != nil {
		s.privateReconnectQueries[copy.SurfaceID] = *copy.ReconnectQuery
	}
	delete(s.privateOfflineStops, copy.SurfaceID)
	if copy.OfflineStop != nil {
		s.privateOfflineStops[copy.SurfaceID] = *copy.OfflineStop
	}
	if v.QueueItems == nil {
		v.QueueItems = map[string]*state.QueueItemRecord{}
	}
	if v.StagedImages == nil {
		v.StagedImages = map[string]*state.StagedImageRecord{}
	}
	if v.StagedFiles == nil {
		v.StagedFiles = map[string]*state.StagedFileRecord{}
	}
	v.DispatchMode = state.DispatchModeNormal
	if err := s.SeedPrivateIDWatermarks(mustPrivateWatermarks(copy)); err != nil {
		return err
	}
	s.nextQueueItemID = max(s.nextQueueItemID, copy.NextQueueID)
	s.nextImageID = max(s.nextImageID, copy.NextImageID)
	s.nextFileID = max(s.nextFileID, copy.NextFileID)
	s.nextRequestCommandID = max(s.nextRequestCommandID, copy.NextCommandID)
	s.nextHeadlessID = max(s.nextHeadlessID, copy.NextHeadlessID)
	s.nextAutoContinueEpisodeID = max(s.nextAutoContinueEpisodeID, copy.NextEpisodeID)
	s.nextShellBindingSequence = max(s.nextShellBindingSequence, copy.NextShellID)
	if len(copy.Holds) > 0 || copy.ActiveQueueItemID != "" || (len(copy.QueuedQueueItemIDs) > 0 && !copy.MigrationRecoveryReleased) || copy.AutoWhip.PendingReason != "" || copy.AutoContinue.Episode != nil {
		v.DispatchMode = state.DispatchModePausedForLocal
		s.privateRecoveryHolds[copy.SurfaceID] = copy
	}
	return nil
}

func privateRecoveryReadOnlyAction(action control.Action) bool {
	switch action.Kind {
	// These mutate only fields in the private surface checkpoint and do not
	// dispatch or change the already frozen queued input plans.
	case control.ActionVerboseCommand, control.ActionPlanCommand, control.ActionDetach, control.ActionWorkspaceDetach, control.ActionStop:
		return true
	case control.ActionPathPickerEnter, control.ActionPathPickerUp, control.ActionPathPickerSelect, control.ActionPathPickerPage, control.ActionPathPickerCancel,
		control.ActionTargetPickerSelectWorkspace, control.ActionTargetPickerSelectSession, control.ActionTargetPickerPage, control.ActionTargetPickerOpenPathPicker, control.ActionTargetPickerCancel, control.ActionTargetPickerBack:
		return true
	case control.ActionShowCommandMenu, control.ActionShowCommandHelp, control.ActionStatus, control.ActionShowHistory, control.ActionHistoryPage, control.ActionHistoryDetail, control.ActionShowThreads, control.ActionShowAllThreads, control.ActionShowScopedThreads, control.ActionShowWorkspaceThreads, control.ActionShowAllWorkspaces, control.ActionShowRecentWorkspaces, control.ActionShowAllThreadWorkspaces, control.ActionShowRecentThreadWorkspaces, control.ActionWorkspaceRoot, control.ActionWorkspaceList, control.ActionListInstances:
		return true
	case control.ActionModeCommand, control.ActionModelCommand, control.ActionReasoningCommand, control.ActionAccessCommand, control.ActionCodexProfileCommand, control.ActionClaudeProfileCommand, control.ActionOpenCodeProfileCommand, control.ActionAutoWhipCommand, control.ActionAutoContinueCommand:
		return len(strings.Fields(action.Text)) <= 1 && action.OptionID == ""
	}
	return false
}

// PrivateRecoveryActionAllowed lets a host keep an unprocessed durable input
// pending before invoking any reducer or attachment preparation.
func (s *Service) PrivateRecoveryActionAllowed(action control.Action) bool {
	_, held := s.privateRecoveryHolds[action.SurfaceSessionID]
	return !held || s.privateRecoveryLocalAction(action)
}

func (s *Service) privateRecoveryLocalAction(action control.Action) bool {
	if action.Kind == control.ActionAccessCommand {
		if surface := s.root.Surfaces[action.SurfaceSessionID]; surface != nil && s.surfaceBackend(surface) != agentproto.BackendOpenCode {
			return true
		}
	}
	return privateRecoveryReadOnlyAction(action)
}
func validCheckpointID(id, prefix string, next int) bool {
	if !strings.HasPrefix(id, prefix) {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, prefix))
	return err == nil && n > 0 && n <= next
}

// PrivateRecoveryCheckpoint returns the original frozen evidence, not a live
// status. There is deliberately no automatic resume operation.
func (s *Service) PrivateRecoveryCheckpoint(surfaceID string) (PrivateCheckpoint, bool) {
	c, ok := s.privateRecoveryHolds[surfaceID]
	if !ok {
		return PrivateCheckpoint{}, false
	}
	raw, _ := json.Marshal(c)
	var clone PrivateCheckpoint
	_ = json.Unmarshal(raw, &clone)
	return clone, true
}

// RestorePrivateNativeProjection may be called only after the host has verified
// the exact live lease and the stored native event/command correlation. It
// reinstates the old core's in-memory rendering bindings, not permission to
// dispatch input or evidence that any process is alive.
func (s *Service) RestorePrivateNativeProjection(surface, instance, command, thread, turn string) error {
	pending, _, _ := s.pendingRemoteBindingRecord(instance)
	active := s.activeRemoteBinding(instance, "")
	for _, existing := range []*remoteTurnBinding{pending, active} {
		if existing == nil {
			continue
		}
		if existing.SurfaceSessionID != surface || existing.CommandID != command || (existing.ThreadID != "" && thread != "" && existing.ThreadID != thread) || (existing.TurnID != "" && turn != "" && existing.TurnID != turn) {
			return fmt.Errorf("native event conflicts with active turn binding")
		}
		return nil
	}
	checkpoint, held := s.privateRecoveryHolds[surface]
	if !held {
		return nil
	}
	match := -1
	for i, h := range checkpoint.Holds {
		if (h.Kind == "pending_remote" || h.Kind == "active_remote") && h.InstanceID == instance && h.CommandID == command && command != "" && (h.ThreadID == "" || thread == "" || h.ThreadID == thread) && (h.TurnID == "" || turn == "" || h.TurnID == turn) {
			if match >= 0 {
				return fmt.Errorf("ambiguous native recovery binding")
			}
			match = i
		}
	}
	if match < 0 {
		return fmt.Errorf("native recovery binding does not match durable command")
	}
	h := checkpoint.Holds[match]
	var binding remoteTurnBinding
	if err := json.Unmarshal(h.Frozen, &binding); err != nil {
		return err
	}
	if binding.SurfaceSessionID != surface || binding.InstanceID != instance || binding.CommandID != command {
		return fmt.Errorf("invalid frozen native binding")
	}
	if h.Kind == "pending_remote" {
		s.turns.bindPendingRemote(instance, &binding)
	} else {
		s.turns.bindActiveRemote(instance, &binding)
	}
	retained := make([]PrivateRecoveryHold, 0, len(checkpoint.Holds))
	for i, v := range checkpoint.Holds {
		if i == match {
			continue
		}
		same := v.InstanceID == instance && (v.ThreadID == "" || thread == "" || v.ThreadID == thread) && (v.TurnID == "" || turn == "" || v.TurnID == turn)
		if same && (v.Kind == "pending_turn_text" || v.Kind == "pending_plan_text") {
			var item completedTextItem
			if err := json.Unmarshal(v.Frozen, &item); err != nil {
				return err
			}
			if item.InstanceID != instance {
				return fmt.Errorf("invalid pending text")
			}
			if v.Kind == "pending_turn_text" {
				s.progress.storePendingTurnText(&item)
			} else {
				s.progress.storePendingPlanProposal(&item)
			}
			continue
		}
		if same && v.Kind == "turn_file_changes" {
			var value turnFileChangeSummary
			if err := json.Unmarshal(v.Frozen, &value); err != nil {
				return err
			}
			s.progress.restorePrivateFileChanges(instance, v.ThreadID, v.TurnID, &value)
			continue
		}
		if same && v.Kind == "turn_diff" {
			var value control.TurnDiffSnapshot
			if err := json.Unmarshal(v.Frozen, &value); err != nil {
				return err
			}
			if value.ThreadID != v.ThreadID || value.TurnID != v.TurnID {
				return fmt.Errorf("invalid frozen turn diff")
			}
			s.progress.restorePrivateTurnDiff(instance, v.ThreadID, v.TurnID, &value)
			continue
		}
		if same && v.Kind == "item_buffer" {
			var saved struct {
				ItemID, ItemKind, TextValue string
				TextChunks                  []string
			}
			if err := json.Unmarshal(v.Frozen, &saved); err != nil {
				return err
			}
			if saved.ItemID == "" {
				return fmt.Errorf("invalid item buffer")
			}
			b := s.ensureItemBuffer(instance, v.ThreadID, v.TurnID, saved.ItemID, saved.ItemKind)
			b.textValue = saved.TextValue
			b.textChunks = append([]string(nil), saved.TextChunks...)
			continue
		}
		retained = append(retained, v)
	}
	checkpoint.Holds = retained
	s.privateRecoveryHolds[surface] = checkpoint
	return nil
}

func mustPrivateWatermarks(c PrivateCheckpoint) PrivateIDWatermarks {
	w, _ := c.IDWatermarks()
	return w
}
