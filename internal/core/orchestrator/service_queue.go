package orchestrator

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Service) replyAnchorForTurn(instanceID, threadID, turnID string) (string, string) {
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(threadID) == "" || strings.TrimSpace(turnID) == "" {
		return "", ""
	}
	binding := s.lookupRemoteTurn(instanceID, threadID, turnID)
	if binding == nil {
		return "", ""
	}
	return strings.TrimSpace(firstNonEmpty(binding.ReplyToMessageID, binding.SourceMessageID)),
		strings.TrimSpace(firstNonEmpty(binding.ReplyToMessagePreview, binding.SourceMessagePreview))
}

func (s *Service) enqueueQueueItem(surface *state.SurfaceConsoleRecord, sourceMessageID, sourceMessagePreview string, relatedMessageIDs []string, inputs []agentproto.Input, threadID, cwd string, routeMode state.RouteMode, overrides state.ModelConfigRecord, front bool) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	item := &state.QueueItemRecord{
		SurfaceSessionID:      surface.SurfaceSessionID,
		SourceKind:            state.QueueItemSourceUser,
		SourceMessageID:       sourceMessageID,
		SourceMessagePreview:  normalizeSourceMessagePreview(sourceMessagePreview),
		SourceMessageIDs:      uniqueStrings(append([]string{sourceMessageID}, relatedMessageIDs...)),
		ReplyToMessageID:      sourceMessageID,
		ReplyToMessagePreview: normalizeSourceMessagePreview(sourceMessagePreview),
		Inputs:                inputs,
		FrozenThreadID:        threadID,
		FrozenCWD:             cwd,
		FrozenOverride:        s.resolveFrozenPromptOverride(inst, surface, threadID, cwd, overrides),
		RouteModeAtEnqueue:    routeMode,
		Status:                state.QueueItemQueued,
	}
	if inst != nil && strings.TrimSpace(threadID) != "" {
		s.recordThreadUserMessage(inst, threadID, sourceMessagePreview)
	}
	return s.enqueuePreparedQueueItem(surface, item, front)
}

func (s *Service) enqueueAutoContinueQueueItem(surface *state.SurfaceConsoleRecord, replyToMessageID, replyToMessagePreview string, inputs []agentproto.Input, threadID, cwd string, routeMode state.RouteMode, overrides state.ModelConfigRecord, front bool) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	item := &state.QueueItemRecord{
		SurfaceSessionID:      surface.SurfaceSessionID,
		SourceKind:            state.QueueItemSourceAutoContinue,
		ReplyToMessageID:      strings.TrimSpace(replyToMessageID),
		ReplyToMessagePreview: normalizeSourceMessagePreview(replyToMessagePreview),
		Inputs:                inputs,
		FrozenThreadID:        threadID,
		FrozenCWD:             cwd,
		FrozenOverride:        s.resolveFrozenPromptOverride(inst, surface, threadID, cwd, overrides),
		RouteModeAtEnqueue:    routeMode,
		Status:                state.QueueItemQueued,
	}
	return s.enqueuePreparedQueueItem(surface, item, front)
}

func (s *Service) enqueuePreparedQueueItem(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, front bool) []control.UIEvent {
	if item == nil || surface == nil {
		return nil
	}
	s.nextQueueItemID++
	itemID := fmt.Sprintf("queue-%d", s.nextQueueItemID)
	item.ID = itemID
	if item.SourceKind == "" {
		item.SourceKind = state.QueueItemSourceUser
	}
	if item.ReplyToMessageID == "" {
		item.ReplyToMessageID = item.SourceMessageID
	}
	if item.ReplyToMessagePreview == "" {
		item.ReplyToMessagePreview = item.SourceMessagePreview
	}
	surface.QueueItems[item.ID] = item
	if front {
		surface.QueuedQueueItemIDs = append([]string{item.ID}, surface.QueuedQueueItemIDs...)
	} else {
		surface.QueuedQueueItemIDs = append(surface.QueuedQueueItemIDs, item.ID)
	}
	position := len(surface.QueuedQueueItemIDs)
	if front {
		position = 1
	}
	var events []control.UIEvent
	events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID:   item.ID,
		Status:        string(item.Status),
		QueuePosition: position,
		QueueOn:       true,
	}, queueItemSourceMessageIDs(item))...)
	return append(events, s.dispatchNext(surface)...)
}

func (s *Service) consumeStagedInputs(surface *state.SurfaceConsoleRecord) ([]agentproto.Input, []string, string) {
	imageKeys := make([]string, 0, len(surface.StagedImages))
	for imageID := range surface.StagedImages {
		imageKeys = append(imageKeys, imageID)
	}
	sort.Strings(imageKeys)
	fileKeys := make([]string, 0, len(surface.StagedFiles))
	for fileID := range surface.StagedFiles {
		fileKeys = append(fileKeys, fileID)
	}
	sort.Strings(fileKeys)

	var inputs []agentproto.Input
	var sourceMessageIDs []string
	for _, imageID := range imageKeys {
		image := surface.StagedImages[imageID]
		if image.State != state.ImageStaged {
			continue
		}
		inputs = append(inputs, agentproto.Input{
			Type:     agentproto.InputLocalImage,
			Path:     image.LocalPath,
			MIMEType: image.MIMEType,
		})
		image.State = state.ImageBound
		sourceMessageIDs = append(sourceMessageIDs, image.SourceMessageID)
	}
	filePrompt := stagedFilePrompt(surface, fileKeys, &sourceMessageIDs)
	return inputs, sourceMessageIDs, filePrompt
}

func stagedFilePrompt(surface *state.SurfaceConsoleRecord, fileKeys []string, sourceMessageIDs *[]string) string {
	if surface == nil || len(fileKeys) == 0 {
		return ""
	}
	lines := []string{"附带参考文件（内容未直接注入上下文，可按需读取以下本地路径）："}
	for _, fileID := range fileKeys {
		file := surface.StagedFiles[fileID]
		if file == nil || file.State != state.FileStaged {
			continue
		}
		path := strings.TrimSpace(file.LocalPath)
		if path == "" {
			continue
		}
		name := strings.TrimSpace(file.FileName)
		if name == "" {
			name = filepath.Base(path)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", name, path))
		file.State = state.FileBound
		*sourceMessageIDs = append(*sourceMessageIDs, file.SourceMessageID)
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func freezeRoute(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord) (threadID, cwd string, routeMode state.RouteMode, createThread bool) {
	switch {
	case surface.RouteMode == state.RouteModeNewThreadReady && strings.TrimSpace(surface.PreparedThreadCWD) != "":
		return "", surface.PreparedThreadCWD, state.RouteModeNewThreadReady, true
	case surface.RouteMode == state.RouteModeFollowLocal && surface.SelectedThreadID != "":
		threadID = surface.SelectedThreadID
		if thread := inst.Threads[threadID]; threadVisible(thread) {
			cwd = thread.CWD
			return threadID, cwd, state.RouteModeFollowLocal, false
		}
	case surface.RouteMode == state.RouteModePinned && surface.SelectedThreadID != "":
		threadID = surface.SelectedThreadID
		if thread := inst.Threads[threadID]; threadVisible(thread) {
			cwd = thread.CWD
			return threadID, cwd, state.RouteModePinned, false
		}
	}
	return "", inst.WorkspaceRoot, surface.RouteMode, false
}

func (s *Service) dispatchNext(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface.DispatchMode != state.DispatchModeNormal || surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) == 0 {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || !inst.Online || inst.ActiveTurnID != "" || s.pendingRemote[inst.InstanceID] != nil {
		return nil
	}
	if s.progress.instanceHasCompact(inst.InstanceID) {
		return nil
	}

	queueID := surface.QueuedQueueItemIDs[0]
	surface.QueuedQueueItemIDs = surface.QueuedQueueItemIDs[1:]
	item := surface.QueueItems[queueID]
	if item == nil || item.Status != state.QueueItemQueued {
		return nil
	}
	item.Status = state.QueueItemDispatching
	surface.ActiveQueueItemID = item.ID
	s.pendingRemote[inst.InstanceID] = &remoteTurnBinding{
		InstanceID:            inst.InstanceID,
		SurfaceSessionID:      surface.SurfaceSessionID,
		QueueItemID:           item.ID,
		SourceMessageID:       item.SourceMessageID,
		SourceMessagePreview:  item.SourceMessagePreview,
		ReplyToMessageID:      firstNonEmpty(item.ReplyToMessageID, item.SourceMessageID),
		ReplyToMessagePreview: firstNonEmpty(item.ReplyToMessagePreview, item.SourceMessagePreview),
		ThreadID:              item.FrozenThreadID,
		ThreadCWD:             item.FrozenCWD,
		Status:                string(item.Status),
	}
	originMessageID := firstNonEmpty(item.SourceMessageID, item.ReplyToMessageID)

	command := &agentproto.Command{
		Kind: agentproto.CommandPromptSend,
		Origin: agentproto.Origin{
			Surface:   surface.SurfaceSessionID,
			UserID:    surface.ActorUserID,
			ChatID:    surface.ChatID,
			MessageID: originMessageID,
		},
		Target: agentproto.Target{
			ThreadID:              item.FrozenThreadID,
			CWD:                   item.FrozenCWD,
			CreateThreadIfMissing: item.FrozenThreadID == "",
		},
		Prompt: agentproto.Prompt{
			Inputs: item.Inputs,
		},
		Overrides: agentproto.PromptOverrides{
			Model:           item.FrozenOverride.Model,
			ReasoningEffort: item.FrozenOverride.ReasoningEffort,
			AccessMode:      item.FrozenOverride.AccessMode,
		},
	}

	events := appendPendingInputTyping(s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOff:    true,
	}, queueItemSourceMessageIDs(item)), item.SourceMessageID, true)
	events = append(events, control.UIEvent{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command:          command,
	})
	return events
}

func (s *Service) markRemoteTurnRunning(instanceID string, initiator agentproto.Initiator, threadID, turnID string) []control.UIEvent {
	binding := s.promotePendingRemote(instanceID, initiator, threadID, turnID)
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil || surface.ActiveQueueItemID == "" {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	if item.FrozenThreadID == "" {
		item.FrozenThreadID = threadID
	}
	inst := s.root.Instances[instanceID]
	if inst != nil {
		targetThreadID := strings.TrimSpace(firstNonEmpty(item.FrozenThreadID, threadID))
		s.recordThreadUserMessage(inst, targetThreadID, item.SourceMessagePreview)
	}
	s.progress.captureRemoteTurnStartTotalUsage(instanceID, binding, item.FrozenThreadID)
	if binding.StartedAt.IsZero() {
		binding.StartedAt = s.now().UTC()
	}
	item.Status = state.QueueItemRunning
	events := s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
	}, queueItemSourceMessageIDs(item))
	if item.FrozenThreadID != "" {
		routeMode := item.RouteModeAtEnqueue
		if routeMode == "" || routeMode == state.RouteModeNewThreadReady {
			routeMode = state.RouteModePinned
		}
		events = append(events, s.bindSurfaceToThreadMode(surface, inst, item.FrozenThreadID, routeMode)...)
	}
	return events
}

func (s *Service) completeRemoteTurn(instanceID, threadID, turnID, status, errorMessage string, problem *agentproto.ErrorInfo, finalText string, summary *control.FileChangeSummary) []control.UIEvent {
	binding := s.lookupRemoteTurn(instanceID, threadID, turnID)
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil || surface.ActiveQueueItemID == "" {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	if status == "failed" || (status != "completed" && strings.TrimSpace(errorMessage) != "") {
		item.Status = state.QueueItemFailed
	} else {
		item.Status = state.QueueItemCompleted
	}
	surface.ActiveQueueItemID = ""
	events := appendPendingInputTyping(s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOff:    true,
	}, queueItemSourceMessageIDs(item)), item.SourceMessageID, false)
	if errorMessage != "" {
		if inst := s.root.Instances[instanceID]; inst != nil {
			s.clearThreadReplay(inst, threadID)
		}
		notice := &control.Notice{
			Code: "turn_failed",
			Text: errorMessage,
		}
		if problem != nil {
			problemNotice := NoticeForProblem(*problem)
			problemNotice.Code = "turn_failed"
			notice = &problemNotice
		}
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	events = append(events, s.dispatchNext(surface)...)
	s.clearRemoteTurn(instanceID, turnID)
	events = append(events, s.finishSurfaceAfterWork(surface)...)
	events = append(events, s.maybeScheduleAutoContinueAfterRemoteTurn(surface, item, turnID, status, problem, finalText, summary)...)
	return events
}

func (s *Service) renderTextItem(instanceID, threadID, turnID, itemID, text string, final bool) []control.UIEvent {
	return s.renderTextItemWithSummary(instanceID, threadID, turnID, itemID, text, final, nil, nil, nil)
}

func (s *Service) renderImageItem(instanceID string, event agentproto.Event) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	thread := (*state.ThreadRecord)(nil)
	if inst != nil && strings.TrimSpace(event.ThreadID) != "" {
		thread = s.ensureThread(inst, event.ThreadID)
		preview := strings.TrimSpace(metadataString(event.Metadata, "revisedPrompt"))
		if preview == "" {
			preview = "已生成图片"
		}
		snippet := previewOfText("图片：" + preview)
		thread.LastAssistantMessage = snippet
		thread.Preview = snippet
		s.touchThread(thread)
	}

	problem := agentproto.ErrorInfo{
		Code:      "image_generation_missing_payload",
		Layer:     "orchestrator",
		Stage:     "image_item_completed",
		Operation: "image_generation",
		Message:   "图片生成结果缺少可发送内容。",
		ThreadID:  event.ThreadID,
		TurnID:    event.TurnID,
		ItemID:    event.ItemID,
	}
	savedPath := strings.TrimSpace(metadataString(event.Metadata, "savedPath"))
	imageBase64 := strings.TrimSpace(metadataString(event.Metadata, "imageBase64"))
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		if inst != nil && (savedPath == "" && imageBase64 == "") && strings.TrimSpace(event.ThreadID) != "" {
			s.storeThreadReplayTurnNotice(inst, event.ThreadID, event.TurnID, NoticeForProblem(problem))
		}
		return nil
	}
	problem.SurfaceSessionID = surface.SurfaceSessionID
	events := []control.UIEvent{}
	if surface.ActiveTurnOrigin != agentproto.InitiatorLocalUI {
		routeMode := surface.RouteMode
		if routeMode != state.RouteModeFollowLocal {
			routeMode = state.RouteModePinned
		}
		if inst != nil {
			events = append(events, s.bindSurfaceToThreadMode(surface, inst, event.ThreadID, routeMode)...)
		}
	}
	if savedPath == "" && imageBase64 == "" {
		notice := NoticeForProblem(problem)
		return append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           &notice,
		})
	}

	replySourceMessageID, replySourceMessagePreview := s.replyAnchorForTurn(instanceID, event.ThreadID, event.TurnID)
	return append(events, control.UIEvent{
		Kind:                 control.UIEventImageOutput,
		SurfaceSessionID:     surface.SurfaceSessionID,
		SourceMessageID:      replySourceMessageID,
		SourceMessagePreview: replySourceMessagePreview,
		ImageOutput: &control.ImageOutput{
			ThreadID:    event.ThreadID,
			TurnID:      event.TurnID,
			ItemID:      event.ItemID,
			Prompt:      metadataString(event.Metadata, "revisedPrompt"),
			SavedPath:   savedPath,
			ImageBase64: imageBase64,
		},
	})
}

func (s *Service) renderTextItemWithSummary(instanceID, threadID, turnID, itemID, text string, final bool, summary *control.FileChangeSummary, turnDiff *control.TurnDiffSnapshot, finalSummary *control.FinalTurnSummary) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	surface := s.turnSurface(instanceID, threadID, turnID)
	if surface == nil {
		if final {
			s.storeThreadReplayText(inst, threadID, turnID, itemID, text)
		}
		return nil
	}
	if final {
		s.clearThreadReplay(inst, threadID)
	}
	return s.renderTextToSurface(surface, inst, threadID, turnID, itemID, text, final, summary, turnDiff, finalSummary)
}

func (s *Service) renderTextToSurface(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID, turnID, itemID, text string, final bool, summary *control.FileChangeSummary, turnDiff *control.TurnDiffSnapshot, finalSummary *control.FinalTurnSummary) []control.UIEvent {
	return s.renderTextToSurfaceWithSource(surface, inst, threadID, turnID, itemID, text, final, summary, turnDiff, finalSummary, "", "")
}

func (s *Service) renderTextToSurfaceWithSource(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID, turnID, itemID, text string, final bool, summary *control.FileChangeSummary, turnDiff *control.TurnDiffSnapshot, finalSummary *control.FinalTurnSummary, sourceMessageID, sourceMessagePreview string) []control.UIEvent {
	if surface == nil {
		return nil
	}
	replySourceMessageID := strings.TrimSpace(sourceMessageID)
	replySourceMessagePreview := strings.TrimSpace(sourceMessagePreview)
	if replySourceMessageID == "" && inst != nil {
		replySourceMessageID, replySourceMessagePreview = s.replyAnchorForTurn(inst.InstanceID, threadID, turnID)
	}
	events := []control.UIEvent{}
	if surface.ActiveTurnOrigin != agentproto.InitiatorLocalUI {
		routeMode := surface.RouteMode
		if routeMode != state.RouteModeFollowLocal {
			routeMode = state.RouteModePinned
		}
		if inst != nil {
			events = append(events, s.bindSurfaceToThreadMode(surface, inst, threadID, routeMode)...)
		}
	}
	instanceKey := ""
	if inst != nil {
		instanceKey = inst.InstanceID
	}
	blocks := s.renderer.PlanAssistantBlocks(surface.SurfaceSessionID, instanceKey, threadID, turnID, itemID, text)
	thread := (*state.ThreadRecord)(nil)
	if inst != nil {
		thread = s.ensureThread(inst, threadID)
	}
	title := displayThreadTitle(inst, thread, threadID)
	themeKey := threadID
	if themeKey == "" {
		themeKey = title
	}
	if len(blocks) == 0 && final && (summary != nil || turnDiff != nil || finalSummary != nil) {
		syntheticText := "已完成。"
		if summary != nil || turnDiff != nil {
			syntheticText = "已完成文件修改。"
		}
		blocks = []render.Block{{
			ID:               itemID + "-summary",
			SurfaceSessionID: surface.SurfaceSessionID,
			InstanceID:       instanceKey,
			ThreadID:         threadID,
			ThreadTitle:      title,
			TurnID:           turnID,
			ItemID:           itemID,
			Kind:             render.BlockAssistantMarkdown,
			Text:             syntheticText,
			ThemeKey:         themeKey,
			Final:            true,
		}}
	}
	lastBlockIndex := len(blocks) - 1
	for i := range blocks {
		block := blocks[i]
		block.ThreadTitle = title
		block.ThemeKey = themeKey
		block.Final = final
		event := control.UIEvent{
			Kind:                 control.UIEventBlockCommitted,
			SurfaceSessionID:     surface.SurfaceSessionID,
			SourceMessageID:      replySourceMessageID,
			SourceMessagePreview: replySourceMessagePreview,
			Block:                &block,
		}
		if final && summary != nil && i == lastBlockIndex {
			event.FileChangeSummary = summary
		}
		if final && turnDiff != nil && i == lastBlockIndex {
			event.TurnDiffSnapshot = turnDiff
		}
		if final && finalSummary != nil && i == lastBlockIndex {
			event.FinalTurnSummary = finalSummary
		}
		events = append(events, event)
	}
	if thread != nil && strings.TrimSpace(text) != "" {
		snippet := previewOfText(text)
		if snippet != "" {
			thread.LastAssistantMessage = snippet
			thread.Preview = snippet
			s.touchThread(thread)
		}
	}
	return events
}

func (s *Service) recordThreadUserMessage(inst *state.InstanceRecord, threadID, text string) {
	if inst == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	snippet := previewOfText(text)
	if snippet == "" {
		return
	}
	thread := s.ensureThread(inst, threadID)
	if strings.TrimSpace(thread.FirstUserMessage) == "" {
		thread.FirstUserMessage = snippet
	}
	thread.LastUserMessage = snippet
	s.touchThread(thread)
}

func (s *Service) trackItemStart(instanceID string, event agentproto.Event) {
	if event.ItemID == "" || !tracksTextItem(event.ItemKind) {
		return
	}
	buf := s.ensureItemBuffer(instanceID, event.ThreadID, event.TurnID, event.ItemID, event.ItemKind)
	if buf.ItemKind == "" {
		buf.ItemKind = event.ItemKind
	}
	if text, _ := event.Metadata["text"].(string); text != "" {
		buf.replaceText(text)
	}
}

func (s *Service) trackItemDelta(instanceID string, event agentproto.Event) {
	if event.ItemID == "" || event.Delta == "" || !tracksTextItem(event.ItemKind) {
		return
	}
	buf := s.ensureItemBuffer(instanceID, event.ThreadID, event.TurnID, event.ItemID, event.ItemKind)
	if buf.ItemKind == "" {
		buf.ItemKind = event.ItemKind
	}
	buf.appendText(event.Delta)
}

func (s *Service) completeItem(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.ItemID == "" {
		return nil
	}
	key := itemBufferKey(instanceID, event.ThreadID, event.TurnID, event.ItemID)
	if event.ItemKind == "file_change" {
		delete(s.itemBuffers, key)
		s.progress.recordTurnFileChanges(instanceID, event)
		return nil
	}
	if isDynamicToolCallItem(event.ItemKind) {
		delete(s.itemBuffers, key)
		return s.renderDynamicToolCallItem(instanceID, event)
	}
	if isImageGenerationItem(event.ItemKind) {
		delete(s.itemBuffers, key)
		return s.renderImageItem(instanceID, event)
	}
	if isContextCompactionItem(event.ItemKind) {
		delete(s.itemBuffers, key)
		return s.progress.renderCompactNotice(instanceID, event)
	}
	buf := s.itemBuffers[key]
	if buf == nil {
		buf = s.ensureItemBuffer(instanceID, event.ThreadID, event.TurnID, event.ItemID, event.ItemKind)
	}
	if buf.ItemKind == "" {
		buf.ItemKind = event.ItemKind
	}
	bufferText := buf.text()
	if text, _ := event.Metadata["text"].(string); text != "" {
		if bufferText == "" || strings.TrimSpace(bufferText) != strings.TrimSpace(text) {
			buf.replaceText(text)
			bufferText = text
		}
		if buf.ItemKind == "" {
			buf.ItemKind = "agent_message"
		}
	}
	delete(s.itemBuffers, key)
	if !rendersTextItem(buf.ItemKind) || strings.TrimSpace(bufferText) == "" {
		return nil
	}
	if buf.ItemKind == "agent_message" {
		return s.storePendingTurnText(instanceID, event.ThreadID, event.TurnID, event.ItemID, buf.ItemKind, bufferText)
	}
	return s.renderTextItem(instanceID, event.ThreadID, event.TurnID, event.ItemID, bufferText, false)
}

func (s *Service) storePendingTurnText(instanceID, threadID, turnID, itemID, itemKind, text string) []control.UIEvent {
	key := turnRenderKey(instanceID, threadID, turnID)
	previous := s.progress.pendingTurnText[key]
	s.progress.pendingTurnText[key] = &completedTextItem{
		InstanceID: instanceID,
		ThreadID:   threadID,
		TurnID:     turnID,
		ItemID:     itemID,
		ItemKind:   itemKind,
		Text:       text,
	}
	if previous == nil {
		return nil
	}
	return s.renderTextItem(previous.InstanceID, previous.ThreadID, previous.TurnID, previous.ItemID, previous.Text, false)
}

func (s *Service) flushPendingTurnText(instanceID, threadID, turnID string, final bool) []control.UIEvent {
	return s.flushPendingTurnTextWithSummary(instanceID, threadID, turnID, final, nil, nil, nil)
}

func (s *Service) flushPendingTurnTextWithSummary(instanceID, threadID, turnID string, final bool, summary *control.FileChangeSummary, turnDiff *control.TurnDiffSnapshot, finalSummary *control.FinalTurnSummary) []control.UIEvent {
	key := turnRenderKey(instanceID, threadID, turnID)
	pending := s.progress.pendingTurnText[key]
	if pending == nil {
		if final && (summary != nil || turnDiff != nil || finalSummary != nil) {
			return s.renderTextItemWithSummary(instanceID, threadID, turnID, "file-change-summary", "", true, summary, turnDiff, finalSummary)
		}
		return nil
	}
	delete(s.progress.pendingTurnText, key)
	return s.renderTextItemWithSummary(pending.InstanceID, pending.ThreadID, pending.TurnID, pending.ItemID, pending.Text, final, summary, turnDiff, finalSummary)
}

func (s *Service) flushPendingTurnTextIfTurnContinues(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.ThreadID == "" || event.TurnID == "" {
		return nil
	}
	if event.Kind == agentproto.EventTurnCompleted {
		return nil
	}
	key := turnRenderKey(instanceID, event.ThreadID, event.TurnID)
	pending := s.progress.pendingTurnText[key]
	if pending == nil {
		return nil
	}
	switch event.Kind {
	case agentproto.EventItemStarted, agentproto.EventItemDelta, agentproto.EventItemCompleted:
		if event.ItemID == pending.ItemID {
			return nil
		}
		return s.flushPendingTurnText(instanceID, event.ThreadID, event.TurnID, false)
	case agentproto.EventTurnPlanUpdated:
		return s.flushPendingTurnText(instanceID, event.ThreadID, event.TurnID, false)
	case agentproto.EventRequestStarted, agentproto.EventRequestResolved:
		return s.flushPendingTurnText(instanceID, event.ThreadID, event.TurnID, false)
	default:
		return nil
	}
}
