package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	defaultThreadHistoryTTL = 10 * time.Minute
	threadHistoryPageSize   = 20
)

type threadHistoryTurnSummary struct {
	TurnID       string
	Ordinal      int
	Status       string
	InputPreview string
	Inputs       []string
	Outputs      []string
	ErrorText    string
	IsCurrent    bool
	UpdatedAt    time.Time
}

func (s *Service) openThreadHistory(surface *state.SurfaceConsoleRecord, sourceMessageID string, inline bool) []eventcontract.Event {
	inst, threadID, noticeCode, noticeText := s.currentThreadHistoryTarget(surface)
	if inst == nil || strings.TrimSpace(threadID) == "" {
		return notice(surface, noticeCode, noticeText)
	}
	s.clearTargetPickerRuntime(surface)
	now := s.now()
	flow := newOwnerCardFlowRecord(ownerCardFlowKindThreadHistory, s.pickers.nextThreadHistoryToken(), firstNonEmpty(surface.ActorUserID), now, defaultThreadHistoryTTL, ownerCardFlowPhaseLoading)
	if inline {
		flow.MessageID = strings.TrimSpace(sourceMessageID)
	}
	record := &activeThreadHistoryRecord{
		ThreadID: strings.TrimSpace(threadID),
		ViewMode: control.FeishuThreadHistoryViewList,
		Page:     0,
	}
	s.setActiveOwnerCardFlow(surface, flow)
	s.setActiveThreadHistory(surface, record)
	return s.startThreadHistoryQuery(surface, inst, flow, record, sourceMessageID, inline)
}

func (s *Service) handleThreadHistoryPage(surface *state.SurfaceConsoleRecord, pickerID string, page int, actorUserID, sourceMessageID string, inline bool) []eventcontract.Event {
	flow, record, blocked := s.requireActiveThreadHistory(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	inst, threadID, noticeCode, noticeText := s.currentThreadHistoryTarget(surface)
	if inst == nil || threadID == "" || threadID != record.ThreadID {
		s.clearThreadHistoryRuntime(surface)
		return notice(surface, firstNonEmpty(noticeCode, "history_expired"), firstNonEmpty(noticeText, "这张历史卡片已经失效，请重新发送 /history。"))
	}
	record.ViewMode = control.FeishuThreadHistoryViewList
	if page < 0 {
		page = 0
	}
	record.Page = page
	record.TurnID = ""
	refreshOwnerCardFlow(flow, ownerCardFlowPhaseLoading, s.now(), defaultThreadHistoryTTL)
	if inline {
		flow.MessageID = strings.TrimSpace(sourceMessageID)
	}
	return s.startThreadHistoryQuery(surface, inst, flow, record, sourceMessageID, inline)
}

func (s *Service) handleThreadHistoryDetail(surface *state.SurfaceConsoleRecord, pickerID, turnID, actorUserID, sourceMessageID string, inline bool) []eventcontract.Event {
	flow, record, blocked := s.requireActiveThreadHistory(surface, pickerID, actorUserID)
	if blocked != nil {
		return blocked
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return notice(surface, "history_turn_missing", "请选择要查看的那一轮后再继续。")
	}
	inst, threadID, noticeCode, noticeText := s.currentThreadHistoryTarget(surface)
	if inst == nil || threadID == "" || threadID != record.ThreadID {
		s.clearThreadHistoryRuntime(surface)
		return notice(surface, firstNonEmpty(noticeCode, "history_expired"), firstNonEmpty(noticeText, "这张历史卡片已经失效，请重新发送 /history。"))
	}
	record.ViewMode = control.FeishuThreadHistoryViewDetail
	record.TurnID = turnID
	refreshOwnerCardFlow(flow, ownerCardFlowPhaseLoading, s.now(), defaultThreadHistoryTTL)
	if inline {
		flow.MessageID = strings.TrimSpace(sourceMessageID)
	}
	return s.startThreadHistoryQuery(surface, inst, flow, record, sourceMessageID, inline)
}

func (s *Service) startThreadHistoryQuery(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, flow *activeOwnerCardFlowRecord, record *activeThreadHistoryRecord, sourceMessageID string, inline bool) []eventcontract.Event {
	if surface == nil || inst == nil || flow == nil || record == nil {
		return nil
	}
	view := s.buildThreadHistoryLoadingView(surface, inst, flow, record)
	if inline {
		view.MessageID = ""
		if sourceMessageID = strings.TrimSpace(sourceMessageID); sourceMessageID != "" {
			flow.MessageID = sourceMessageID
		}
	}
	events := []eventcontract.Event{s.threadHistoryViewEvent(surface, view, inline, sourceMessageID)}
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindDaemonCommand,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  sourceMessageID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandThreadHistoryRead,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			SourceMessageID:  sourceMessageID,
			InstanceID:       inst.InstanceID,
			ThreadID:         record.ThreadID,
		},
	})
	return events
}

func (s *Service) currentThreadHistoryTarget(surface *state.SurfaceConsoleRecord) (*state.InstanceRecord, string, string, string) {
	if surface == nil {
		return nil, "", "history_unavailable", "当前飞书会话不可用，暂时无法查看历史。"
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil || !inst.Online {
		return nil, "", "history_unavailable", "当前还没有接管在线实例，暂时无法查看历史。"
	}
	threadID, _, routeMode, createThread := freezeRoute(inst, surface)
	if createThread {
		return nil, "", "history_no_thread", "当前还没有可查看的会话历史。直接发送文本开启新会话后，再试 /history。"
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		switch routeMode {
		case state.RouteModeFollowLocal:
			return nil, "", "history_no_thread", "当前还没有跟随到可查看的会话，请先在 VS Code 中进入一个会话后再试 /history。"
		case state.RouteModePinned:
			return nil, "", "history_no_thread", "当前选中的会话已经不可用，请重新发送 /use 后再试 /history。"
		default:
			return nil, "", "history_no_thread", "当前还没有选中的会话，请先发送 /use，或直接发送文本开启新会话。"
		}
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return nil, "", "history_no_thread", "当前会话暂时不可见，请重新发送 /use 后再试 /history。"
	}
	return inst, threadID, "", ""
}

func (s *Service) requireActiveThreadHistory(surface *state.SurfaceConsoleRecord, pickerID, actorUserID string) (*activeOwnerCardFlowRecord, *activeThreadHistoryRecord, []eventcontract.Event) {
	flow, blocked := s.requireActiveOwnerCardFlow(surface, ownerCardFlowKindThreadHistory, pickerID, actorUserID, "这张历史卡片已失效，请重新发送 /history。", "这张历史卡片只允许发起者本人操作。")
	if blocked != nil {
		return nil, nil, blocked
	}
	record := s.activeThreadHistory(surface)
	if record == nil {
		s.clearSurfaceOwnerCardFlow(surface)
		return nil, nil, notice(surface, "history_expired", "这张历史卡片已失效，请重新发送 /history。")
	}
	return flow, record, nil
}

func (s *Service) RecordThreadHistoryMessage(surfaceID, pickerID, messageID string) {
	s.RecordOwnerCardFlowMessage(surfaceID, pickerID, messageID)
}

func (s *Service) HandleSurfaceThreadHistoryLoaded(surfaceID string) []eventcontract.Event {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	flow := s.activeOwnerCardFlow(surface)
	record := s.activeThreadHistory(surface)
	if surface == nil || flow == nil || flow.Kind != ownerCardFlowKindThreadHistory || record == nil {
		return nil
	}
	inst, threadID, noticeCode, noticeText := s.currentThreadHistoryTarget(surface)
	if inst == nil || threadID == "" || threadID != record.ThreadID {
		view := s.buildThreadHistoryErrorView(surface, nil, flow, record, firstNonEmpty(noticeCode, "history_expired"), firstNonEmpty(noticeText, "这张历史卡片已经失效，请重新发送 /history。"))
		s.clearThreadHistoryRuntime(surface)
		return []eventcontract.Event{s.threadHistoryViewEvent(surface, view, false, "")}
	}
	flow.Phase = ownerCardFlowPhaseResolved
	bumpOwnerCardFlowRevision(flow)
	view := s.buildThreadHistoryResolvedView(surface, inst, flow, record)
	return []eventcontract.Event{s.threadHistoryViewEvent(surface, view, false, "")}
}

func (s *Service) HandleSurfaceThreadHistoryFailure(surfaceID, code, text string) []eventcontract.Event {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: strings.TrimSpace(surfaceID),
			Notice: &control.Notice{
				Code: code,
				Text: text,
			},
		}}
	}
	flow := s.activeOwnerCardFlow(surface)
	record := s.activeThreadHistory(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindThreadHistory || record == nil {
		return notice(surface, code, text)
	}
	flow.Phase = ownerCardFlowPhaseError
	bumpOwnerCardFlowRevision(flow)
	view := s.buildThreadHistoryErrorView(surface, nil, flow, record, code, text)
	return []eventcontract.Event{s.threadHistoryViewEvent(surface, view, false, "")}
}

func (s *Service) buildThreadHistoryLoadingView(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, flow *activeOwnerCardFlowRecord, record *activeThreadHistoryRecord) control.FeishuThreadHistoryView {
	return control.FeishuThreadHistoryView{
		PickerID:       strings.TrimSpace(flow.FlowID),
		MessageID:      strings.TrimSpace(flow.MessageID),
		Mode:           record.ViewMode,
		Title:          "历史记录",
		ThreadID:       strings.TrimSpace(record.ThreadID),
		ThreadLabel:    s.threadHistoryThreadLabel(inst, record.ThreadID),
		Loading:        true,
		LoadingText:    "正在读取当前会话历史，请稍候...",
		NoticeSections: []control.FeishuCardTextSection{{Label: "当前状态", Lines: []string{"正在读取当前会话历史，请稍候..."}}},
		CreatedAt:      flow.CreatedAt,
		ExpiresAt:      flow.ExpiresAt,
		SelectedTurnID: strings.TrimSpace(record.TurnID),
	}
}

func (s *Service) buildThreadHistoryResolvedView(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, flow *activeOwnerCardFlowRecord, record *activeThreadHistoryRecord) control.FeishuThreadHistoryView {
	history := s.SurfaceThreadHistory(surface.SurfaceSessionID)
	if history == nil {
		return s.buildThreadHistoryErrorView(surface, inst, flow, record, "history_unavailable", "还没有拿到可展示的历史结果，请稍后重试。")
	}
	summaries := buildThreadHistoryTurnSummaries(*history, strings.TrimSpace(inst.ActiveTurnID))
	view := control.FeishuThreadHistoryView{
		PickerID:         strings.TrimSpace(flow.FlowID),
		MessageID:        strings.TrimSpace(flow.MessageID),
		Mode:             record.ViewMode,
		Title:            "历史记录",
		ThreadID:         firstNonEmpty(strings.TrimSpace(history.Thread.ThreadID), strings.TrimSpace(record.ThreadID)),
		ThreadLabel:      s.threadHistoryResolvedLabel(inst, history),
		TurnCount:        len(summaries),
		SelectedTurnID:   strings.TrimSpace(record.TurnID),
		CreatedAt:        flow.CreatedAt,
		ExpiresAt:        flow.ExpiresAt,
		CurrentTurnLabel: threadHistoryCurrentTurnLabel(summaries),
	}
	switch record.ViewMode {
	case control.FeishuThreadHistoryViewDetail:
		summary, index := findThreadHistoryTurnSummary(summaries, record.TurnID)
		if summary == nil {
			return s.buildThreadHistoryErrorView(surface, inst, flow, record, "history_turn_missing", "当前选择的那一轮已经不可用了，请返回列表重新选择。")
		}
		view.Detail = &control.FeishuThreadHistoryTurnDetail{
			TurnID:      summary.TurnID,
			Ordinal:     summary.Ordinal,
			Status:      summary.Status,
			ErrorText:   summary.ErrorText,
			Inputs:      append([]string(nil), summary.Inputs...),
			Outputs:     append([]string(nil), summary.Outputs...),
			ReturnPage:  index / threadHistoryPageSize,
			UpdatedText: humanizeThreadHistoryTime(s.now(), summary.UpdatedAt),
		}
		if index > 0 {
			view.Detail.PrevTurnID = summaries[index-1].TurnID
		}
		if index+1 < len(summaries) {
			view.Detail.NextTurnID = summaries[index+1].TurnID
		}
	default:
		page, totalPages, start, end := paginateThreadHistory(record.Page, len(summaries))
		record.Page = page
		view.Mode = control.FeishuThreadHistoryViewList
		view.Page = page
		view.TotalPages = totalPages
		view.PageStart = start
		view.PageEnd = end
		if len(summaries) == 0 {
			view.Hint = "这个会话暂时还没有可展示的历史。"
			return view
		}
		options := make([]control.FeishuThreadHistoryTurnOption, 0, end-start)
		for _, summary := range summaries[start:end] {
			options = append(options, control.FeishuThreadHistoryTurnOption{
				TurnID:   summary.TurnID,
				Label:    threadHistoryTurnOptionLabel(summary),
				MetaText: humanizeThreadHistoryTime(s.now(), summary.UpdatedAt),
				Current:  summary.IsCurrent,
			})
		}
		view.TurnOptions = options
		view.Hint = "先在下拉里选中一轮，再查看详情。"
	}
	return view
}

func (s *Service) buildThreadHistoryErrorView(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, flow *activeOwnerCardFlowRecord, record *activeThreadHistoryRecord, code, text string) control.FeishuThreadHistoryView {
	threadID := ""
	pickerID := ""
	messageID := ""
	mode := control.FeishuThreadHistoryViewList
	page := 0
	turnID := ""
	createdAt := time.Time{}
	expiresAt := time.Time{}
	if flow != nil {
		pickerID = strings.TrimSpace(flow.FlowID)
		messageID = strings.TrimSpace(flow.MessageID)
		createdAt = flow.CreatedAt
		expiresAt = flow.ExpiresAt
	}
	if record != nil {
		threadID = strings.TrimSpace(record.ThreadID)
		mode = record.ViewMode
		page = record.Page
		turnID = strings.TrimSpace(record.TurnID)
	}
	return control.FeishuThreadHistoryView{
		PickerID:       pickerID,
		MessageID:      messageID,
		Mode:           mode,
		Title:          "历史记录",
		ThreadID:       threadID,
		ThreadLabel:    s.threadHistoryThreadLabel(inst, threadID),
		Page:           page,
		SelectedTurnID: turnID,
		NoticeCode:     strings.TrimSpace(code),
		NoticeText:     strings.TrimSpace(text),
		NoticeSections: threadHistoryNoticeSections(code, text),
		CreatedAt:      createdAt,
		ExpiresAt:      expiresAt,
	}
}

func threadHistoryNoticeSections(code, text string) []control.FeishuCardTextSection {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	label := "说明"
	if strings.TrimSpace(code) != "" {
		label = "错误"
	}
	return []control.FeishuCardTextSection{{
		Label: label,
		Lines: []string{text},
	}}
}

func (s *Service) threadHistoryThreadLabel(inst *state.InstanceRecord, threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if inst == nil || threadID == "" {
		return threadID
	}
	return displayThreadTitle(inst, inst.Threads[threadID], threadID)
}

func (s *Service) threadHistoryResolvedLabel(inst *state.InstanceRecord, history *agentproto.ThreadHistoryRecord) string {
	if history == nil {
		return ""
	}
	if inst != nil {
		label := s.threadHistoryThreadLabel(inst, history.Thread.ThreadID)
		if strings.TrimSpace(label) != "" {
			return label
		}
	}
	thread := &state.ThreadRecord{
		ThreadID: history.Thread.ThreadID,
		Name:     strings.TrimSpace(history.Thread.Name),
		CWD:      strings.TrimSpace(history.Thread.CWD),
	}
	return displayThreadTitle(syntheticPersistedThreadInstance(thread, agentproto.BackendCodex), thread, history.Thread.ThreadID)
}

func buildThreadHistoryTurnSummaries(history agentproto.ThreadHistoryRecord, currentTurnID string) []threadHistoryTurnSummary {
	if len(history.Turns) == 0 {
		return nil
	}
	summaries := make([]threadHistoryTurnSummary, 0, len(history.Turns))
	currentTurnID = strings.TrimSpace(currentTurnID)
	for index := len(history.Turns) - 1; index >= 0; index-- {
		turn := history.Turns[index]
		summary := threadHistoryTurnSummary{
			TurnID:    strings.TrimSpace(turn.TurnID),
			Ordinal:   index + 1,
			Status:    strings.TrimSpace(turn.Status),
			ErrorText: strings.TrimSpace(turn.ErrorMessage),
			IsCurrent: currentTurnID != "" && strings.TrimSpace(turn.TurnID) == currentTurnID,
			UpdatedAt: latestThreadHistoryTurnTime(turn),
		}
		for _, item := range turn.Items {
			text := strings.TrimSpace(item.Text)
			switch strings.TrimSpace(item.Kind) {
			case "user_message":
				if text != "" {
					summary.Inputs = append(summary.Inputs, text)
				}
			case "agent_message":
				if text != "" {
					summary.Outputs = append(summary.Outputs, text)
				}
			case "command_execution":
				command := strings.TrimSpace(item.Command)
				if command == "" {
					command = text
				}
				if command == "" {
					continue
				}
				if item.ExitCode != nil {
					summary.Outputs = append(summary.Outputs, fmt.Sprintf("[命令] %s (exit %d)", command, *item.ExitCode))
				} else {
					summary.Outputs = append(summary.Outputs, "[命令] "+command)
				}
			}
		}
		summary.InputPreview = threadHistoryInputPreview(summary.Inputs)
		if summary.Status == "" {
			summary.Status = "-"
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func latestThreadHistoryTurnTime(turn agentproto.ThreadHistoryTurnRecord) time.Time {
	if !turn.CompletedAt.IsZero() {
		return turn.CompletedAt
	}
	if !turn.StartedAt.IsZero() {
		return turn.StartedAt
	}
	return time.Time{}
}

func threadHistoryInputPreview(inputs []string) string {
	if len(inputs) == 0 {
		return "-"
	}
	preview := truncateThreadHistoryText(inputs[0], 24)
	if len(inputs) > 1 {
		return fmt.Sprintf("%s 等 %d 条", preview, len(inputs))
	}
	return preview
}

func threadHistoryTurnOptionLabel(summary threadHistoryTurnSummary) string {
	label := fmt.Sprintf("#%d | %s | %s", summary.Ordinal, firstNonEmpty(strings.TrimSpace(summary.Status), "-"), firstNonEmpty(strings.TrimSpace(summary.InputPreview), "-"))
	if summary.IsCurrent {
		return "当前 · " + label
	}
	return label
}

func threadHistoryCurrentTurnLabel(summaries []threadHistoryTurnSummary) string {
	for _, summary := range summaries {
		if summary.IsCurrent {
			return fmt.Sprintf("第 %d 轮", summary.Ordinal)
		}
	}
	return ""
}

func findThreadHistoryTurnSummary(summaries []threadHistoryTurnSummary, turnID string) (*threadHistoryTurnSummary, int) {
	turnID = strings.TrimSpace(turnID)
	for index := range summaries {
		if strings.TrimSpace(summaries[index].TurnID) == turnID {
			return &summaries[index], index
		}
	}
	return nil, -1
}

func paginateThreadHistory(page, total int) (clampedPage, totalPages, start, end int) {
	if total <= 0 {
		return 0, 1, 0, 0
	}
	if page < 0 {
		page = 0
	}
	totalPages = (total + threadHistoryPageSize - 1) / threadHistoryPageSize
	if page >= totalPages {
		page = totalPages - 1
	}
	start = page * threadHistoryPageSize
	end = start + threadHistoryPageSize
	if end > total {
		end = total
	}
	return page, totalPages, start, end
}

func humanizeThreadHistoryTime(now, value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return humanizeRelativeTime(now, value)
}

func truncateThreadHistoryText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 3 {
		limit = 3
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-3]) + "..."
}

func (s *Service) clearThreadHistoryRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	s.clearSurfaceThreadHistory(surface)
	flow := s.activeOwnerCardFlow(surface)
	if flow != nil && flow.Kind == ownerCardFlowKindThreadHistory {
		s.clearSurfaceOwnerCardFlow(surface)
	}
}
