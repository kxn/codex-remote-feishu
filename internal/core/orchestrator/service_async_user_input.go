package orchestrator

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type asyncUserInputQuestion struct {
	Title   string   `json:"title"`
	Options []string `json:"options"`
}

// Async questions are ordinary messages, not RPCs waiting for request.respond.
// Keep their drafts separate from both PendingRequests and the active owner flow.
type asyncUserInputRecord struct {
	ID          string
	InstanceID  string
	ThreadID    string
	ActorUserID string
	Questions   []asyncUserInputQuestion
	Answers     []string
	Revision    int
	Submitted   bool
}

func (s *Service) presentAsyncUserInput(instanceID string, event agentproto.Event) ([]eventcontract.Event, bool) {
	if event.ItemKind != "agent_message" || event.Metadata["delivery"] != "async" || event.ThreadID == "" || event.TurnID == "" {
		return nil, false
	}
	var questions []asyncUserInputQuestion
	raw, err := json.Marshal(event.Metadata["questions"])
	if err != nil || json.Unmarshal(raw, &questions) != nil || len(questions) == 0 {
		return nil, false
	}
	for _, question := range questions {
		if strings.TrimSpace(question.Title) == "" {
			return nil, false
		}
		for _, option := range question.Options {
			if strings.TrimSpace(option) == "" {
				return nil, false
			}
		}
	}
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || surface.AttachedInstanceID != instanceID {
		return nil, false
	}
	binding := s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID)
	var events []eventcontract.Event
	if surface.SelectedThreadID != event.ThreadID {
		if binding == nil || !binding.BootstrapNewThread || binding.ThreadCommitted || surface.RouteMode != state.RouteModeNewThreadReady {
			return nil, false
		}
		events = s.maybeBindSurfaceForRemoteTurn(surface, s.root.Instances[instanceID], instanceID, event.ThreadID, event.TurnID)
		if surface.SelectedThreadID != event.ThreadID {
			return nil, false
		}
	}
	runtime := s.ensureSurfaceUIRuntime(surface)
	if runtime.AsyncUserInputs == nil {
		runtime.AsyncUserInputs = map[string]*asyncUserInputRecord{}
	}
	id := itemBufferKey(instanceID, event.ThreadID, event.TurnID, event.ItemID)
	if runtime.AsyncUserInputs[id] != nil {
		return nil, true
	}
	actorUserID := surface.ActorUserID
	if binding != nil {
		actorUserID = queuedItemActorUserID(surface.QueueItems[binding.QueueItemID], surface)
	}
	record := &asyncUserInputRecord{
		ID: id, InstanceID: instanceID, ThreadID: event.ThreadID, ActorUserID: actorUserID,
		Questions: questions, Revision: 1,
	}
	runtime.AsyncUserInputs[id] = record
	eventOut := asyncUserInputPageEvent(surface, record, "", "")
	eventOut.SourceMessageID, eventOut.SourceMessagePreview = s.replyAnchorForTurn(instanceID, event.ThreadID, event.TurnID)
	if eventOut.SourceMessageID != "" {
		eventOut.Meta.MessageDelivery = eventcontract.ReplyThreadAppendOnlyDelivery()
	}
	return append(events, eventOut), true
}

func asyncUserInputPayload(record *asyncUserInputRecord, optionID string) map[string]any {
	return map[string]any{
		frontstagecontract.CardActionPayloadKeyKind:            frontstagecontract.CardActionKindAsyncUserInput,
		frontstagecontract.CardActionPayloadKeyRequestID:       record.ID,
		frontstagecontract.CardActionPayloadKeyRequestRevision: record.Revision,
		frontstagecontract.CardActionPayloadKeyOptionID:        optionID,
	}
}

func asyncUserInputPageEvent(surface *state.SurfaceConsoleRecord, record *asyncUserInputRecord, messageID, status string) eventcontract.Event {
	view := control.FeishuPageView{
		PageID: "async_user_input", Title: "请回答", ThemeKey: record.ThreadID,
		Interactive: true, Patchable: true, MessageID: messageID,
		SuppressDefaultRelatedButtons: true,
	}
	for i, answer := range record.Answers {
		view.BodySections = append(view.BodySections, control.FeishuCardTextSection{Label: record.Questions[i].Title, Lines: []string{answer}})
	}
	if record.Submitted {
		view.Sealed = true
		view.Interactive = false
	} else {
		question := record.Questions[len(record.Answers)]
		view.BodySections = append(view.BodySections, control.FeishuCardTextSection{Label: fmt.Sprintf("问题 %d/%d", len(record.Answers)+1, len(record.Questions)), Lines: []string{question.Title}})
		entry := control.CommandCatalogEntry{}
		for i, option := range question.Options {
			entry.Buttons = append(entry.Buttons, control.CommandCatalogButton{
				Label: option, Kind: control.CommandCatalogButtonCallbackAction,
				CallbackValue: asyncUserInputPayload(record, strconv.Itoa(i)),
			})
		}
		entry.Form = &control.CommandCatalogForm{
			SubmitLabel: "提交回答", SubmitValue: asyncUserInputPayload(record, ""),
			Field: control.CommandCatalogFormField{Name: "answer", Kind: control.CommandCatalogFormFieldText, Label: "补充或其他回答", Placeholder: "输入回答"},
		}
		view.Sections = []control.CommandCatalogSection{{Entries: []control.CommandCatalogEntry{entry}}}
	}
	if status != "" {
		view.NoticeSections = []control.FeishuCardTextSection{{Lines: []string{status}}}
	}
	event := eventcontract.NewEventFromPayload(eventcontract.PagePayload{View: control.NormalizeFeishuPageView(view)}, eventcontract.EventMeta{Target: eventcontract.TargetRef{SurfaceSessionID: surface.SurfaceSessionID}})
	event.InlineReplaceCurrentCard = messageID != ""
	return event
}

func (s *Service) handleAsyncUserInput(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	runtime := s.surfaceUIRuntimeState(surface)
	if runtime == nil || action.Request == nil {
		return notice(surface, "async_question_expired", "这个问题卡片已失效，请在当前会话直接回复。")
	}
	record := runtime.AsyncUserInputs[action.Request.RequestID]
	if record == nil || record.Submitted || record.Revision != action.Request.RequestRevision ||
		record.ActorUserID != action.ActorUserID || surface.AttachedInstanceID != record.InstanceID || surface.SelectedThreadID != record.ThreadID ||
		(surface.RouteMode != state.RouteModePinned && surface.RouteMode != state.RouteModeFollowLocal) {
		return notice(surface, "async_question_expired", "这个问题卡片已失效或已回答，请在当前会话直接回复。")
	}
	answer := ""
	question := record.Questions[len(record.Answers)]
	if action.Request.RequestOptionID != "" {
		index, err := strconv.Atoi(action.Request.RequestOptionID)
		if err != nil || index < 0 || index >= len(question.Options) {
			return notice(surface, "async_question_invalid", "请选择卡片中的选项或输入回答。")
		}
		answer = question.Options[index]
	} else if values := action.Request.Answers["answer"]; len(values) == 1 {
		answer = strings.TrimSpace(values[0])
	}
	if answer == "" {
		return notice(surface, "async_question_empty", "请输入回答或选择一个选项。")
	}
	// Check before advancing the draft so a blocked final answer remains editable.
	if len(record.Answers)+1 == len(record.Questions) {
		if blocked := s.asyncUserInputDispatchBlocked(surface, record); len(blocked) != 0 {
			return blocked
		}
	}
	record.Answers = append(record.Answers, answer)
	record.Revision++
	if len(record.Answers) < len(record.Questions) {
		return []eventcontract.Event{asyncUserInputPageEvent(surface, record, action.MessageID, "")}
	}
	record.Submitted = true
	text := make([]string, 0, len(record.Questions)*2)
	for i, question := range record.Questions {
		text = append(text, "问题："+question.Title, "回答："+record.Answers[i])
	}
	inst := s.root.Instances[record.InstanceID]
	cwd := inst.Threads[record.ThreadID].CWD
	plan := agentproto.DefaultPromptDispatchPlanForExecutionThread(record.ThreadID)
	plan.CWD = cwd
	item := &state.QueueItemRecord{
		SurfaceSessionID: surface.SurfaceSessionID, ActorUserID: action.ActorUserID, SourceKind: state.QueueItemSourceUser,
		SourceMessageID: action.MessageID, SourceMessagePreview: normalizeSourceMessagePreview(strings.Join(text, "\n")), SourceMessageIDs: []string{action.MessageID},
		Inputs:             []agentproto.Input{{Type: agentproto.InputText, Text: strings.Join(text, "\n")}},
		FrozenDispatchPlan: agentproto.NormalizePromptDispatchPlan(plan),
		FrozenOverride:     s.resolveFrozenPromptOverride(inst, surface, record.ThreadID, cwd, state.ModelConfigRecord{}),
		FrozenPlanMode:     s.freezePlanModeForPrompt(surface), CodexAdmissionRef: s.freezeCodexAdmissionRefForPrompt(surface),
		CodexConnectionContract: s.freezeCodexConnectionContractForPrompt(surface), CodexThreadPolicy: s.freezeCodexThreadPolicyForPrompt(surface),
		RouteModeAtEnqueue: surface.RouteMode, Status: state.QueueItemQueued,
	}
	events := []eventcontract.Event{asyncUserInputPageEvent(surface, record, action.MessageID, "回答已进入发送队列。")}
	events = append(events, s.enqueuePreparedQueueItem(surface, item, false)...)
	if _, threadID, turnID, ok := s.activeSteerTarget(surface); ok && surface.DispatchMode == state.DispatchModeNormal && threadID == record.ThreadID && item.Status == state.QueueItemQueued && !s.surfaceHasPendingSteer(surface) {
		for index, id := range surface.QueuedQueueItemIDs {
			if id == item.ID {
				events = append(events, s.dispatchSteerCandidates(surface, inst, threadID, turnID, []steerCandidate{{Item: item, QueueIndex: index}}, action.MessageID, "")...)
				break
			}
		}
	}
	return events
}

func (s *Service) asyncUserInputDispatchBlocked(surface *state.SurfaceConsoleRecord, record *asyncUserInputRecord) []eventcontract.Event {
	if surface.ActiveRequestCapture != nil || activePendingRequest(surface) != nil {
		return notice(surface, "request_pending", "请先处理当前确认请求，再回答这个问题。")
	}
	inst := s.root.Instances[record.InstanceID]
	if inst == nil || !inst.Online || !threadVisible(inst.Threads[record.ThreadID]) {
		return notice(surface, "async_question_unavailable", "原会话暂不可用，请恢复连接后重试。")
	}
	if surface.ContractRefreshPending && !s.surfaceInstanceCompatibleForAttach(surface, inst) {
		return notice(surface, "contract_refresh_pending", "机器人配置已变化，本卡尚未提交。请等当前任务结束后，直接在聊天中发送回答以完成配置切换，或恢复原配置后重试本卡。")
	}
	if blocked := s.blockFeishuRoomActiveDispatch(surface); blocked != nil {
		return blocked
	}
	thread := inst.Threads[record.ThreadID]
	model, _ := s.codexTargetModelForGroupSwitch(inst, surface, record.ThreadID, thread.CWD, state.ModelConfigRecord{}, surface.CodexThreadPolicy)
	sourceGroup, targetGroup := codexModelGroup(codexThreadModelForGroupSwitch(thread)), codexModelGroup(model)
	if sourceGroup != "" && targetGroup != "" && sourceGroup != targetGroup {
		return notice(surface, "async_question_model_changed", "当前模型设置与原会话不兼容，请恢复原模型后回答。")
	}
	return nil
}
