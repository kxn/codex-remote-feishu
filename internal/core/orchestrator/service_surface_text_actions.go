package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) handleText(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	text := strings.TrimSpace(action.Text)
	pendingText := text
	pendingInputs := action.Inputs
	if text == "" && len(action.Inputs) == 0 {
		return nil
	}
	if blocked := s.blockFeishuRoomNoWorkspaceDataPlane(surface, action); blocked != nil {
		return blocked
	}

	if surface.ActiveRequestCapture != nil {
		if text == "" {
			return notice(surface, "request_capture_waiting_text", "当前反馈模式只接受文本，请发送一条文字处理意见。")
		}
		return s.consumeCapturedRequestFeedback(surface, action, text)
	}
	if pending := activePendingRequest(surface); pending != nil {
		return notice(surface, "request_pending", pendingRequestNoticeText(pending))
	}
	if events, handled := s.handleReviewSessionText(surface, action, text); handled {
		return events
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		if s.surfaceIsHeadless(surface) && surfaceFeishuRoomID(surface) != "" {
			if blocked := s.blockFeishuRoomActiveDispatch(surface); blocked != nil {
				return blocked
			}
			workspaceKey := s.surfaceCurrentWorkspaceKey(surface)
			if workspaceKey == "" {
				// Save message for replay after the user selects a workspace
				// and thread from the picker.
				s.storePendingTextInput(surface, text, action.Inputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
				return s.openTargetPickerForAction(surface, action, "", nil, action.MessageID, false)
			}
			targetBackend := s.surfaceBackend(surface)
			continuation := s.buildHeadlessWorkspaceContinuation(surface, workspaceKey, targetBackend, false)
			resolution := s.resolveWorkspaceContract(surface, workspaceKey, targetBackend)
			events := s.executeResolvedWorkspaceContinuation(surface, continuation, resolution, attachWorkspaceOptions{SuppressAutoUsePrompt: true})
			inst = s.root.Instances[surface.AttachedInstanceID]
			if inst == nil {
				// Instance is starting — save message for replay when it
				// connects and the surface attaches.
				s.storePendingTextInput(surface, text, action.Inputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
				return events
			}
			return append(events, s.handleText(surface, action)...)
		}
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if surface.ContractRefreshPending && !s.surfaceInstanceCompatibleForAttach(surface, inst) {
		if surface.PendingHeadless != nil || s.surfaceHasLiveRemoteWork(surface) {
			return notice(surface, "contract_refresh_pending", "机器人配置刚发生变化，当前会话仍在处理中，稍后会自动切换到新配置。")
		}
		surface.ContractRefreshPending = false
		events := s.reconcileHeadlessSurfaceContract(surface)
		if len(events) == 0 {
			return notice(surface, "contract_refresh_unavailable", "当前会话暂时无法自动切换到新配置，请稍后重试，或 /detach 后重新连接。")
		}
		s.storePendingTextInput(surface, text, action.Inputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
		return events
	}
	dynamicCodexPreset, fixedCodexProfile := s.codexMessagePresetProfileMode(surface, inst)
	requestOverride := surface.PromptOverride
	presetKey := ""
	if (dynamicCodexPreset || fixedCodexProfile) && strings.TrimSpace(action.Text) != "" {
		originalText := action.Text
		cleanText, preset, explicit, parseErr := parseCodexMessagePreset(originalText)
		if fixedCodexProfile {
			if explicit {
				fixedModel := ""
				if profile, ok := s.surfaceCodexProfileSummary(surface); ok {
					fixedModel, _ = fixedCodexAPIProfileModel(profile)
				}
				return notice(surface, "codex_message_preset_fixed_profile", fmt.Sprintf("当前 Codex Profile 使用固定模型 %s，不能使用单次消息模型前缀。", fixedModel))
			}
		} else if explicit {
			if parseErr != nil {
				return notice(surface, "codex_message_preset_invalid", parseErr.Error()+"。"+codexMessagePresetUsage)
			}

			if problem := codexMessagePresetCatalogProblem(inst, preset); problem != "" {
				return notice(surface, "codex_message_preset_model_unavailable", problem)
			}
			pendingText = originalText
			text = cleanText
			action.Text = cleanText
			action.Inputs = stripCodexMessagePresetFromCurrentInputs(action.Inputs, action.SteerInputs, preset)
			requestOverride = codexMessagePresetOverride(surface, preset)
			if explicit {
				if failed := s.setCodexTopicOverride(surface, state.CodexPromptOverrideRecord{Model: preset.Model, ReasoningEffort: preset.ReasoningEffort}); failed != nil {
					return failed
				}
			}
			presetKey = preset.Key
		}
	}
	detour, detourProblem := s.resolveDetourDirective(surface, inst, text)
	if detourProblem != "" {
		return notice(surface, "detour_invalid", detourProblem)
	}
	text = detour.CleanText
	sanitizedAction := action
	sanitizedAction.Text = text
	if detour.Triggered {
		sanitizedAction.Inputs = stripDetourInputs(action.Inputs)
	}
	if !detour.Triggered && presetKey == "" {
		if autoSteer := s.maybeAutoSteerReply(surface, sanitizedAction); autoSteer != nil {
			return append(s.maybeSealPlanProposalForInput(surface), autoSteer...)
		}
	}
	if blocked := s.blockFeishuRoomActiveDispatch(surface); blocked != nil {
		return blocked
	}
	if !detour.Triggered {
		if blocked := s.maybePrepareImplicitNewThreadFromUnboundText(surface, inst, text); blocked != nil {
			// Save the message for replay after the user resolves the
			// blocking condition (e.g., selects a thread from the picker).
			s.storePendingTextInput(surface, pendingText, pendingInputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
			return blocked
		}
		if blocked := s.unboundInputBlocked(surface); blocked != nil {
			s.storePendingTextInput(surface, pendingText, pendingInputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
			return blocked
		}
		if surface.RouteMode == state.RouteModeNewThreadReady && s.preparedNewThreadHasPendingCreate(surface) {
			return notice(surface, "new_thread_first_input_pending", "当前新会话的首条消息已经在排队或发送中；请等待它落地后再继续发送。")
		}
	}

	threadID, cwd, routeMode, createThread := freezeRoute(inst, surface)
	if detour.Triggered {
		threadID = ""
		cwd, routeMode = freezeDetourRoute(inst, surface)
		createThread = false
	}
	inputs, stagedMessageIDs, filePrompt := s.consumeStagedInputs(surface, action.ActorUserID)
	if filePrompt != "" {
		inputs = append(inputs, agentproto.Input{Type: agentproto.InputText, Text: filePrompt})
	}
	messageInputs := append([]agentproto.Input{}, sanitizedAction.Inputs...)
	if len(messageInputs) == 0 {
		if text != "" {
			messageInputs = []agentproto.Input{{Type: agentproto.InputText, Text: text}}
		} else if detour.Triggered && len(inputs) == 0 {
			s.restoreStagedInputs(surface, stagedMessageIDs)
			return notice(surface, "detour_empty_prompt", detourEmptyPromptText)
		}
	}
	inputs = append(inputs, messageInputs...)
	if !detour.Triggered && !createThread && threadID == "" {
		s.restoreStagedInputs(surface, stagedMessageIDs)
		return notice(surface, "thread_not_ready", "当前还没有可发送的目标会话。请先 /use 重新选择会话；headless 模式可直接发送文本开启新会话（也可 /new 先进入待命），如需跟随 VS Code 请先 /mode vscode 再 /follow。")
	}
	if strings.TrimSpace(cwd) == "" {
		s.restoreStagedInputs(surface, stagedMessageIDs)
		if detour.Triggered {
			return notice(surface, "detour_cwd_missing", "当前无法获取临时会话的工作目录，请先重新选择工作区或会话。")
		}
		if createThread {
			return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
		}
		return notice(surface, "thread_not_ready", "当前还没有可发送的目标会话。请先 /use 重新选择会话；headless 模式可直接发送文本开启新会话（也可 /new 先进入待命），如需跟随 VS Code 请先 /mode vscode 再 /follow。")
	}
	events := s.maybeSealPlanProposalForInput(surface)
	if detour.Triggered {
		if presetKey != "" {
			return append(events, s.enqueueCodexMessagePresetQueueItemWithTarget(
				surface,
				action.MessageID,
				text,
				stagedMessageIDs,
				inputs,
				threadID,
				cwd,
				routeMode,
				requestOverride,
				presetKey,
				detour.ExecutionMode,
				detour.SourceThreadID,
				detour.SurfaceBindingPolicy,
				"",
				false,
			)...)
		}
		return append(events, s.enqueueQueueItemWithTarget(
			surface,
			action.MessageID,
			text,
			stagedMessageIDs,
			inputs,
			threadID,
			cwd,
			routeMode,
			surface.PromptOverride,
			detour.ExecutionMode,
			detour.SourceThreadID,
			detour.SurfaceBindingPolicy,
			"",
			false,
		)...)
	}
	// Clear any saved pending input — the user sent a new message that is
	// being processed, so the old pending is stale.
	s.clearPendingTextInput(surface)
	if presetKey != "" {
		return append(events, s.enqueueCodexMessagePresetQueueItem(surface, action.MessageID, text, stagedMessageIDs, inputs, threadID, cwd, routeMode, requestOverride, presetKey, false)...)
	}
	return append(events, s.enqueueQueueItem(surface, action.MessageID, action.Text, stagedMessageIDs, inputs, threadID, cwd, routeMode, surface.PromptOverride, false)...)
}
