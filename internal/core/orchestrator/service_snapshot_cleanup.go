package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) observeConfig(inst *state.InstanceRecord, threadID, cwd, scope, model, effort, access string) {
	if inst == nil {
		return
	}
	cwd = state.NormalizeWorkspaceKey(cwd)
	workspaceKey := state.ResolveWorkspaceKey(inst.WorkspaceKey, inst.WorkspaceRoot, cwd)
	cwdDefaultKey := firstNonEmpty(cwd, workspaceKey)
	access = agentproto.NormalizeAccessMode(access)
	switch scope {
	case "cwd_default":
		s.updateInstanceCWDDefaults(inst, cwdDefaultKey, func(current *state.ModelConfigRecord) {
			if model != "" {
				current.Model = model
			}
			if effort != "" {
				current.ReasoningEffort = effort
			}
			if access != "" {
				current.AccessMode = access
			}
		})
		if workspaceKey == "" || isVSCodeInstance(inst) {
			return
		}
		s.updateWorkspaceDefaults(workspaceKey, func(current *state.ModelConfigRecord) {
			if model != "" {
				current.Model = model
			}
			if effort != "" {
				current.ReasoningEffort = effort
			}
			if access != "" {
				current.AccessMode = access
			}
		})
	default:
		if threadID == "" && access == "" {
			return
		}
		if threadID != "" {
			thread := s.ensureThread(inst, threadID)
			if cwd != "" {
				thread.CWD = cwd
			}
			if model != "" {
				thread.ExplicitModel = model
			}
			if effort != "" {
				thread.ExplicitReasoningEffort = effort
			}
		}
		if access != "" && isVSCodeInstance(inst) {
			s.updateInstanceCWDDefaults(inst, cwdDefaultKey, func(current *state.ModelConfigRecord) {
				current.AccessMode = access
			})
		}
		if access != "" && workspaceKey != "" && !isVSCodeInstance(inst) {
			s.updateWorkspaceDefaults(workspaceKey, func(current *state.ModelConfigRecord) {
				current.AccessMode = access
			})
		}
	}
}

func (s *Service) updateInstanceCWDDefaults(inst *state.InstanceRecord, cwd string, apply func(*state.ModelConfigRecord)) {
	cwd = state.NormalizeWorkspaceKey(cwd)
	if inst == nil || cwd == "" || apply == nil {
		return
	}
	if inst.CWDDefaults == nil {
		inst.CWDDefaults = map[string]state.ModelConfigRecord{}
	}
	current := compactModelConfig(inst.CWDDefaults[cwd])
	apply(&current)
	current = compactModelConfig(current)
	if modelConfigRecordEmpty(current) {
		delete(inst.CWDDefaults, cwd)
		return
	}
	inst.CWDDefaults[cwd] = current
}

func (s *Service) discardDrafts(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	var events []control.UIEvent
	for _, image := range surface.StagedImages {
		if image.State != state.ImageStaged {
			continue
		}
		image.State = state.ImageDiscarded
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: image.ImageID,
			Status:      string(image.State),
			QueueOff:    true,
			ThumbsDown:  true,
		}, []string{image.SourceMessageID})...)
	}
	for _, file := range surface.StagedFiles {
		if file.State != state.FileStaged {
			continue
		}
		file.State = state.FileDiscarded
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: file.FileID,
			Status:      string(file.State),
			QueueOff:    true,
			ThumbsDown:  true,
		}, []string{file.SourceMessageID})...)
	}
	for _, queueID := range append([]string{}, surface.QueuedQueueItemIDs...) {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued {
			continue
		}
		item.Status = state.QueueItemDiscarded
		s.markImagesForMessages(surface, queueItemSourceMessageIDs(item), state.ImageDiscarded)
		s.markFilesForMessages(surface, queueItemSourceMessageIDs(item), state.FileDiscarded)
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: item.ID,
			Status:      string(item.Status),
			QueueOff:    true,
			ThumbsDown:  true,
		}, queueItemSourceMessageIDs(item))...)
	}
	surface.QueuedQueueItemIDs = nil
	surface.StagedImages = map[string]*state.StagedImageRecord{}
	surface.StagedFiles = map[string]*state.StagedFileRecord{}
	return events
}

func (s *Service) discardStagedInputsForRouteChange(surface *state.SurfaceConsoleRecord, prevThreadID string, prevRouteMode state.RouteMode, nextThreadID string, nextRouteMode state.RouteMode) []control.UIEvent {
	if surface == nil {
		return nil
	}
	prevThreadID = strings.TrimSpace(prevThreadID)
	nextThreadID = strings.TrimSpace(nextThreadID)
	if prevThreadID == nextThreadID && prevRouteMode == nextRouteMode {
		return nil
	}
	discarded := 0
	var events []control.UIEvent
	for imageID, image := range surface.StagedImages {
		if image == nil || image.State != state.ImageStaged {
			continue
		}
		image.State = state.ImageDiscarded
		discarded++
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: imageID,
			Status:      string(image.State),
			QueueOff:    true,
			ThumbsDown:  true,
		}, []string{image.SourceMessageID})...)
		delete(surface.StagedImages, imageID)
	}
	for fileID, file := range surface.StagedFiles {
		if file == nil || file.State != state.FileStaged {
			continue
		}
		file.State = state.FileDiscarded
		discarded++
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: fileID,
			Status:      string(file.State),
			QueueOff:    true,
			ThumbsDown:  true,
		}, []string{file.SourceMessageID})...)
		delete(surface.StagedFiles, fileID)
	}
	if discarded == 0 {
		return nil
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: "staged_inputs_discarded_on_route_change",
			Text: fmt.Sprintf("由于输入目标发生变化，已丢弃 %d 个尚未绑定的附件。", discarded),
		},
	})
	return events
}

func (s *Service) maybePromoteWorkspaceRoot(inst *state.InstanceRecord, cwd string) {
	if inst == nil {
		return
	}
	cwd = state.NormalizeWorkspaceKey(cwd)
	if cwd == "" {
		return
	}
	currentRoot := state.NormalizeWorkspaceKey(inst.WorkspaceRoot)
	switch {
	case currentRoot == "":
		currentRoot = cwd
	}
	inst.WorkspaceRoot = currentRoot
	inst.WorkspaceKey = state.ResolveWorkspaceKey(currentRoot)
	inst.ShortName = state.WorkspaceShortName(inst.WorkspaceKey)
	if inst.DisplayName == "" {
		inst.DisplayName = inst.ShortName
	}
}

func (s *Service) retargetManagedHeadlessInstance(inst *state.InstanceRecord, cwd string) {
	cwd = state.NormalizeWorkspaceKey(cwd)
	if inst == nil || cwd == "" || !isHeadlessInstance(inst) {
		return
	}
	inst.WorkspaceRoot = cwd
	inst.WorkspaceKey = state.ResolveWorkspaceKey(cwd)
	inst.ShortName = state.WorkspaceShortName(cwd)
	inst.DisplayName = inst.ShortName
}
