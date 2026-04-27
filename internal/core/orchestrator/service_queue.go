package orchestrator

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func defaultPromptExecutionModeForThread(threadID string) agentproto.PromptExecutionMode {
	if strings.TrimSpace(threadID) == "" {
		return agentproto.PromptExecutionModeStartNew
	}
	return agentproto.PromptExecutionModeResumeExisting
}

func defaultSurfaceBindingPolicy() agentproto.SurfaceBindingPolicy {
	return agentproto.SurfaceBindingPolicyFollowExecutionThread
}

func (s *Service) maybeBindSurfaceForRemoteTurn(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, instanceID, threadID, turnID string) []eventcontract.Event {
	if surface == nil || inst == nil || surface.ActiveTurnOrigin == agentproto.InitiatorLocalUI {
		return nil
	}
	binding := s.lookupRemoteTurn(instanceID, threadID, turnID)
	if remoteBindingKeepsSurfaceSelection(binding) {
		return nil
	}
	targetThreadID := strings.TrimSpace(threadID)
	if binding != nil {
		targetThreadID = remoteBindingSurfaceThreadID(binding)
	}
	if targetThreadID == "" {
		return nil
	}
	routeMode := surface.RouteMode
	if routeMode != state.RouteModeFollowLocal {
		routeMode = state.RouteModePinned
	}
	return s.bindSurfaceToThreadMode(surface, inst, targetThreadID, routeMode)
}

func (s *Service) replyAnchorForTurn(instanceID, threadID, turnID string) (string, string) {
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(threadID) == "" || strings.TrimSpace(turnID) == "" {
		return "", ""
	}
	binding := s.lookupRemoteTurn(instanceID, threadID, turnID)
	if binding == nil {
		if _, session := s.reviewSessionSurface(instanceID, threadID); session != nil {
			return strings.TrimSpace(session.SourceMessageID), ""
		}
		return "", ""
	}
	return strings.TrimSpace(firstNonEmpty(binding.ReplyToMessageID, binding.SourceMessageID)),
		strings.TrimSpace(firstNonEmpty(binding.ReplyToMessagePreview, binding.SourceMessagePreview))
}

func (s *Service) enqueueQueueItem(surface *state.SurfaceConsoleRecord, sourceMessageID, sourceMessagePreview string, relatedMessageIDs []string, inputs []agentproto.Input, threadID, cwd string, routeMode state.RouteMode, overrides state.ModelConfigRecord, front bool) []eventcontract.Event {
	return s.enqueueQueueItemWithTarget(
		surface,
		sourceMessageID,
		sourceMessagePreview,
		relatedMessageIDs,
		inputs,
		threadID,
		cwd,
		routeMode,
		overrides,
		"",
		"",
		"",
		front,
	)
}

func (s *Service) enqueueQueueItemWithTarget(surface *state.SurfaceConsoleRecord, sourceMessageID, sourceMessagePreview string, relatedMessageIDs []string, inputs []agentproto.Input, threadID, cwd string, routeMode state.RouteMode, overrides state.ModelConfigRecord, executionMode agentproto.PromptExecutionMode, sourceThreadID string, bindingPolicy agentproto.SurfaceBindingPolicy, front bool) []eventcontract.Event {
	inst := s.root.Instances[surface.AttachedInstanceID]
	item := &state.QueueItemRecord{
		SurfaceSessionID:           surface.SurfaceSessionID,
		SourceKind:                 state.QueueItemSourceUser,
		SourceMessageID:            sourceMessageID,
		SourceMessagePreview:       normalizeSourceMessagePreview(sourceMessagePreview),
		SourceMessageIDs:           uniqueStrings(append([]string{sourceMessageID}, relatedMessageIDs...)),
		ReplyToMessageID:           sourceMessageID,
		ReplyToMessagePreview:      normalizeSourceMessagePreview(sourceMessagePreview),
		Inputs:                     inputs,
		FrozenThreadID:             threadID,
		FrozenCWD:                  cwd,
		FrozenExecutionMode:        defaultPromptExecutionModeForThread(threadID),
		FrozenSourceThreadID:       "",
		FrozenSurfaceBindingPolicy: defaultSurfaceBindingPolicy(),
		FrozenOverride:             s.resolveFrozenPromptOverride(inst, surface, threadID, cwd, overrides),
		FrozenPlanMode:             state.NormalizePlanModeSetting(surface.PlanMode),
		RouteModeAtEnqueue:         routeMode,
		Status:                     state.QueueItemQueued,
	}
	if mode := agentproto.NormalizePromptExecutionMode(executionMode); mode != "" {
		item.FrozenExecutionMode = mode
	}
	if sourceThreadID = strings.TrimSpace(sourceThreadID); sourceThreadID != "" {
		item.FrozenSourceThreadID = sourceThreadID
	}
	if policy := agentproto.NormalizeSurfaceBindingPolicy(bindingPolicy); policy != "" {
		item.FrozenSurfaceBindingPolicy = policy
	}
	if inst != nil && strings.TrimSpace(threadID) != "" {
		s.recordThreadUserMessage(inst, threadID, sourceMessagePreview)
	}
	return s.enqueuePreparedQueueItem(surface, item, front)
}

func (s *Service) enqueueAutoWhipQueueItem(surface *state.SurfaceConsoleRecord, replyToMessageID, replyToMessagePreview string, inputs []agentproto.Input, threadID, cwd string, routeMode state.RouteMode, overrides state.ModelConfigRecord, front bool) []eventcontract.Event {
	inst := s.root.Instances[surface.AttachedInstanceID]
	item := &state.QueueItemRecord{
		SurfaceSessionID:           surface.SurfaceSessionID,
		SourceKind:                 state.QueueItemSourceAutoWhip,
		ReplyToMessageID:           strings.TrimSpace(replyToMessageID),
		ReplyToMessagePreview:      normalizeSourceMessagePreview(replyToMessagePreview),
		Inputs:                     inputs,
		FrozenThreadID:             threadID,
		FrozenCWD:                  cwd,
		FrozenExecutionMode:        defaultPromptExecutionModeForThread(threadID),
		FrozenSourceThreadID:       "",
		FrozenSurfaceBindingPolicy: defaultSurfaceBindingPolicy(),
		FrozenOverride:             s.resolveFrozenPromptOverride(inst, surface, threadID, cwd, overrides),
		FrozenPlanMode:             state.NormalizePlanModeSetting(surface.PlanMode),
		RouteModeAtEnqueue:         routeMode,
		Status:                     state.QueueItemQueued,
	}
	return s.enqueuePreparedQueueItem(surface, item, front)
}

func (s *Service) enqueuePreparedQueueItem(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, front bool) []eventcontract.Event {
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
	var events []eventcontract.Event
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

func (s *Service) dispatchNext(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface.DispatchMode != state.DispatchModeNormal || surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) == 0 {
		if surface.DispatchMode != state.DispatchModeNormal || surface.ActiveQueueItemID != "" {
			return nil
		}
		return s.maybeDispatchPendingAutoContinue(surface, s.now())
	}
	if autoContinue := s.maybeDispatchPendingAutoContinue(surface, s.now()); len(autoContinue) != 0 {
		return autoContinue
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || !inst.Online || inst.ActiveTurnID != "" || s.turns.pendingRemote[inst.InstanceID] != nil {
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
	s.turns.pendingRemote[inst.InstanceID] = &remoteTurnBinding{
		InstanceID:            inst.InstanceID,
		SurfaceSessionID:      surface.SurfaceSessionID,
		QueueItemID:           item.ID,
		SourceMessageID:       item.SourceMessageID,
		SourceMessagePreview:  item.SourceMessagePreview,
		ReplyToMessageID:      firstNonEmpty(item.ReplyToMessageID, item.SourceMessageID),
		ReplyToMessagePreview: firstNonEmpty(item.ReplyToMessagePreview, item.SourceMessagePreview),
		ExecutionMode:         item.FrozenExecutionMode,
		ThreadID:              strings.TrimSpace(item.FrozenThreadID),
		SourceThreadID:        queuedItemSourceThreadID(item),
		SurfaceBindingPolicy:  queuedItemSurfaceBindingPolicy(item),
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
			ExecutionMode:        item.FrozenExecutionMode,
			SourceThreadID:       item.FrozenSourceThreadID,
			SurfaceBindingPolicy: item.FrozenSurfaceBindingPolicy,
			ThreadID:             item.FrozenThreadID,
			CWD:                  item.FrozenCWD,
			CreateThreadIfMissing: item.FrozenExecutionMode == agentproto.PromptExecutionModeStartNew ||
				(item.FrozenExecutionMode == "" && item.FrozenThreadID == ""),
		},
		Prompt: agentproto.Prompt{
			Inputs: item.Inputs,
		},
		Overrides: agentproto.PromptOverrides{
			Model:           item.FrozenOverride.Model,
			ReasoningEffort: item.FrozenOverride.ReasoningEffort,
			AccessMode:      item.FrozenOverride.AccessMode,
			PlanMode:        string(state.NormalizePlanModeSetting(item.FrozenPlanMode)),
		},
	}

	events := appendPendingInputTyping(s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOff:    true,
	}, queueItemSourceMessageIDs(item)), item.SourceMessageID, true)
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command:          command,
	})
	return events
}

func (s *Service) markRemoteTurnRunning(instanceID string, initiator agentproto.Initiator, threadID, turnID string) []eventcontract.Event {
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
	if !remoteBindingKeepsSurfaceSelection(binding) {
		routeMode := item.RouteModeAtEnqueue
		if routeMode == "" || routeMode == state.RouteModeNewThreadReady {
			routeMode = state.RouteModePinned
		}
		targetThreadID := remoteBindingSurfaceThreadID(binding)
		if targetThreadID == "" {
			targetThreadID = strings.TrimSpace(item.FrozenThreadID)
		}
		events = append(events, s.bindSurfaceToThreadMode(surface, inst, targetThreadID, routeMode)...)
	}
	return events
}

func (s *Service) completeRemoteTurn(outcome *remoteTurnOutcome) []eventcontract.Event {
	if outcome == nil || outcome.Binding == nil || outcome.Surface == nil || outcome.Item == nil {
		return nil
	}
	item := outcome.Item
	surface := outcome.Surface
	switch outcome.Cause {
	case terminalCauseCompleted, terminalCauseUserInterrupted:
		item.Status = state.QueueItemCompleted
	default:
		item.Status = state.QueueItemFailed
	}
	surface.ActiveQueueItemID = ""
	events := appendPendingInputTyping(s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOff:    true,
	}, queueItemSourceMessageIDs(item)), item.SourceMessageID, false)

	handledByAutoContinueCard := false
	switch {
	case outcome.Binding.AutoContinueEpisodeID != "" && outcome.Cause == terminalCauseUserInterrupted:
		events = append(events, s.cancelAutoContinueEpisode(surface)...)
		handledByAutoContinueCard = true
	case outcome.Binding.AutoContinueEpisodeID != "" && outcome.Cause != terminalCauseCompleted && outcome.Cause != terminalCauseAutoContinueEligible:
		if episode := activeAutoContinueEpisode(surface); episode != nil && strings.TrimSpace(episode.EpisodeID) == strings.TrimSpace(outcome.Binding.AutoContinueEpisodeID) {
			if outcome.AnyOutputSeen {
				episode.NoticeMessageID = ""
				episode.NoticeAppendSeq = 0
			}
			episode.LastProblem = cloneProblem(outcome.Problem)
			episode.State = state.AutoContinueEpisodeFailed
			events = append(events, s.autoContinueFailureEvent(surface, episode))
			handledByAutoContinueCard = true
		}
	case outcome.Cause == terminalCauseAutoContinueEligible:
		autoContinueEvents := s.maybeScheduleAutoContinueAfterOutcome(outcome)
		events = append(events, autoContinueEvents...)
		handledByAutoContinueCard = len(autoContinueEvents) != 0
	}

	if !handledByAutoContinueCard && outcome.Cause != terminalCauseCompleted && outcome.Cause != terminalCauseUserInterrupted {
		if inst := s.root.Instances[outcome.InstanceID]; inst != nil {
			s.clearThreadReplay(inst, outcome.ThreadID)
		}
		events = append(events, s.remoteTurnFailureEvent(outcome))
	}

	if outcome.Cause == terminalCauseCompleted {
		s.finishAutoContinueEpisode(outcome)
	}
	events = append(events, s.maybeScheduleAutoWhipAfterRemoteTurn(surface, item, outcome.TurnID, outcome.Cause, outcome.FinalText, outcome.Summary)...)
	s.clearRemoteTurn(outcome.InstanceID, outcome.TurnID)
	return events
}

func (s *Service) renderTextItem(instanceID, threadID, turnID, itemID, text string, final bool) []eventcontract.Event {
	return s.renderTextItemWithSummary(instanceID, threadID, turnID, itemID, text, final, nil, nil, nil)
}

func (s *Service) renderImageItem(instanceID string, event agentproto.Event) []eventcontract.Event {
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
	events := []eventcontract.Event{}
	events = append(events, s.maybeBindSurfaceForRemoteTurn(surface, inst, instanceID, event.ThreadID, event.TurnID)...)
	if savedPath == "" && imageBase64 == "" {
		notice := NoticeForProblem(problem)
		return append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           &notice,
		})
	}

	replySourceMessageID, replySourceMessagePreview := s.replyAnchorForTurn(instanceID, event.ThreadID, event.TurnID)
	outbound := eventcontract.Event{
		Kind:                 eventcontract.KindImageOutput,
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
	}
	if strings.TrimSpace(replySourceMessageID) != "" {
		outbound.Meta.MessageDelivery = eventcontract.ReplyThreadAppendOnlyDelivery()
	}
	return append(events, outbound)
}

func (s *Service) renderTextItemWithSummary(instanceID, threadID, turnID, itemID, text string, final bool, summary *control.FileChangeSummary, turnDiff *control.TurnDiffSnapshot, finalSummary *control.FinalTurnSummary) []eventcontract.Event {
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

func (s *Service) renderTextToSurface(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID, turnID, itemID, text string, final bool, summary *control.FileChangeSummary, turnDiff *control.TurnDiffSnapshot, finalSummary *control.FinalTurnSummary) []eventcontract.Event {
	return s.renderTextToSurfaceWithSource(surface, inst, threadID, turnID, itemID, text, final, summary, turnDiff, finalSummary, "", "")
}

func (s *Service) renderTextToSurfaceWithSource(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID, turnID, itemID, text string, final bool, summary *control.FileChangeSummary, turnDiff *control.TurnDiffSnapshot, finalSummary *control.FinalTurnSummary, sourceMessageID, sourceMessagePreview string) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	replySourceMessageID := strings.TrimSpace(sourceMessageID)
	replySourceMessagePreview := strings.TrimSpace(sourceMessagePreview)
	if replySourceMessageID == "" && inst != nil {
		replySourceMessageID, replySourceMessagePreview = s.replyAnchorForTurn(inst.InstanceID, threadID, turnID)
	}
	instanceKey := ""
	if inst != nil {
		instanceKey = inst.InstanceID
	}
	events := []eventcontract.Event{}
	events = append(events, s.maybeBindSurfaceForRemoteTurn(surface, inst, instanceKey, threadID, turnID)...)
	detourLabel := remoteBindingDetourLabel(s.lookupRemoteTurn(instanceKey, threadID, turnID))
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
		block.DetourLabel = detourLabel
		event := eventcontract.Event{
			Kind:                 eventcontract.KindBlockCommitted,
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

func (s *Service) completeItem(instanceID string, event agentproto.Event) []eventcontract.Event {
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
		if buf.ItemKind == "plan" && strings.TrimSpace(bufferText) != "" {
			return s.storePendingPlanProposal(instanceID, event.ThreadID, event.TurnID, event.ItemID, buf.ItemKind, bufferText)
		}
		return nil
	}
	if buf.ItemKind == "agent_message" {
		return s.storePendingTurnText(instanceID, event.ThreadID, event.TurnID, event.ItemID, buf.ItemKind, bufferText)
	}
	return s.renderTextItem(instanceID, event.ThreadID, event.TurnID, event.ItemID, bufferText, false)
}

func (s *Service) storePendingTurnText(instanceID, threadID, turnID, itemID, itemKind, text string) []eventcontract.Event {
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

func (s *Service) flushPendingTurnText(instanceID, threadID, turnID string, final bool) []eventcontract.Event {
	return s.flushPendingTurnTextWithSummary(instanceID, threadID, turnID, final, nil, nil, nil)
}

func (s *Service) flushPendingTurnTextWithSummary(instanceID, threadID, turnID string, final bool, summary *control.FileChangeSummary, turnDiff *control.TurnDiffSnapshot, finalSummary *control.FinalTurnSummary) []eventcontract.Event {
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

func (s *Service) flushPendingTurnTextIfTurnContinues(instanceID string, event agentproto.Event) []eventcontract.Event {
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
