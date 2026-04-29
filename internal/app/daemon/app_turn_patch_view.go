package daemon

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	turnpatchruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/turnpatchruntime"
	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

var turnPatchRefusalStrongPhrases = []string{
	"我无法协助",
	"我无法帮助",
	"我不能协助",
	"我不能帮助",
	"我不会帮",
	"我无法提供",
	"我必须拒绝",
	"i cannot assist",
	"i can't assist",
	"i'm unable to assist",
	"i cannot help",
	"i can't help",
	"i'm unable to help",
	"i must decline",
	"i must refuse",
	"against my guidelines",
	"against my policy",
	"violates my",
	"i won't help",
	"i won't assist",
	"as an ai",
	"as a language model",
	"i apologize, but i",
	"i'm sorry, but i can't",
}

var turnPatchRefusalWeakHeadKeywords = []string{
	"抱歉",
	"很抱歉",
	"对不起",
	"不好意思",
	"我无法",
	"我不能",
	"不允许",
	"禁止",
	"sorry",
	"apologize",
	"i cannot",
	"i can't",
	"i'm unable",
	"unable to",
	"not permitted",
	"not allowed",
	"refuse to",
}

var turnPatchPlaceholderPhrases = []string{
	"请提供下一步指令",
	"请提供更多细节",
	"请提供更具体的信息",
	"请补充更多细节",
	"我需要更多上下文",
	"无法继续当前任务",
	"please provide the next instruction",
	"please provide more details",
	"please provide more context",
	"please share more context",
	"i need more context",
	"i need more details",
	"unable to continue",
}

func turnPatchNoticeEvent(surfaceID, code, text string) eventcontract.Event {
	return eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		Notice: &control.Notice{
			Code: strings.TrimSpace(code),
			Text: strings.TrimSpace(text),
		},
	}
}

func turnPatchDetectCandidates(preview *codexstate.TurnPatchPreview) []turnpatchruntime.Candidate {
	if preview == nil {
		return nil
	}
	candidates := make([]turnpatchruntime.Candidate, 0, len(preview.Messages))
	for idx, message := range preview.Messages {
		text := strings.TrimSpace(message.Text)
		if text == "" {
			continue
		}
		kind, matchedAt, ok := turnPatchDetectCandidateKind(text)
		if !ok {
			continue
		}
		label := turnPatchCandidateLabel(kind)
		candidates = append(candidates, turnpatchruntime.Candidate{
			CandidateID:   fmt.Sprintf("candidate-%d", idx+1),
			MessageKey:    strings.TrimSpace(message.MessageKey),
			Kind:          kind,
			Label:         label,
			Excerpt:       turnPatchExcerptAround(text, matchedAt, 84),
			DefaultText:   turnPatchDefaultReplacement(kind, text),
			OriginalText:  text,
			QuestionID:    fmt.Sprintf("replacement_%s", strings.TrimSpace(message.MessageKey)),
			QuestionTitle: fmt.Sprintf("候选 %d：%s", idx+1, label),
		})
	}
	return candidates
}

func turnPatchDetectCandidateKind(text string) (turnpatchruntime.CandidateKind, int, bool) {
	normalized := turnPatchCompactText(text)
	if normalized == "" {
		return "", 0, false
	}
	lower := strings.ToLower(normalized)
	for _, phrase := range turnPatchRefusalStrongPhrases {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			return turnpatchruntime.CandidateKindRefusal, utf8.RuneCountInString(lower[:idx]), true
		}
	}
	head := turnPatchPrefixRunes(lower, 150)
	for _, keyword := range turnPatchRefusalWeakHeadKeywords {
		if idx := strings.Index(head, keyword); idx >= 0 {
			return turnpatchruntime.CandidateKindRefusal, utf8.RuneCountInString(head[:idx]), true
		}
	}
	for _, phrase := range turnPatchPlaceholderPhrases {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			return turnpatchruntime.CandidateKindPlaceholder, utf8.RuneCountInString(lower[:idx]), true
		}
	}
	return "", 0, false
}

func turnPatchCandidateLabel(kind turnpatchruntime.CandidateKind) string {
	switch kind {
	case turnpatchruntime.CandidateKindPlaceholder:
		return "疑似占位回复"
	default:
		return "疑似拒绝回复"
	}
}

func turnPatchDefaultReplacement(kind turnpatchruntime.CandidateKind, original string) string {
	if turnPatchLooksCJK(original) {
		switch kind {
		case turnpatchruntime.CandidateKindPlaceholder:
			return "好的，我已理解当前任务，下面直接继续给出可执行结果。"
		default:
			return "好的，我会继续基于当前上下文直接推进，并给出下一步可执行结果。"
		}
	}
	switch kind {
	case turnpatchruntime.CandidateKindPlaceholder:
		return "Understood. I will continue from the current context and provide the concrete next result."
	default:
		return "Understood. I will continue from the current context and provide the next actionable result."
	}
}

func turnPatchExcerptAround(text string, matchAt, limit int) string {
	text = turnPatchCompactText(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit || limit <= 0 {
		return text
	}
	if matchAt < 0 {
		matchAt = 0
	}
	if matchAt > len(runes) {
		matchAt = len(runes)
	}
	prefix := limit / 3
	start := matchAt - prefix
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(runes) {
		end = len(runes)
		start = end - limit
		if start < 0 {
			start = 0
		}
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

func turnPatchRequestEvent(surfaceID, sourceMessageID string, view control.FeishuRequestView, inlineReplace bool) eventcontract.Event {
	event := surfaceRequestPayloadEvent(
		surfaceID,
		eventcontract.RequestPayload{
			View: control.NormalizeFeishuRequestView(view),
		},
		inlineReplace,
	)
	event.SourceMessageID = strings.TrimSpace(sourceMessageID)
	return event
}

func turnPatchRequestView(flow *turnpatchruntime.FlowRecord) control.FeishuRequestView {
	if flow == nil {
		return control.FeishuRequestView{}
	}
	questions := make([]control.RequestPromptQuestion, 0, len(flow.Candidates))
	for _, candidate := range flow.Candidates {
		answer := strings.TrimSpace(flow.Answers[candidate.QuestionID])
		defaultValue := strings.TrimSpace(candidate.DefaultText)
		if answer != "" {
			defaultValue = answer
		}
		questions = append(questions, control.RequestPromptQuestion{
			ID:           strings.TrimSpace(candidate.QuestionID),
			Header:       strings.TrimSpace(candidate.QuestionTitle),
			Question:     "命中片段：" + strings.TrimSpace(candidate.Excerpt),
			AllowOther:   true,
			Placeholder:  "可直接提交预填模板，也可以改成更贴合当前上下文的替换文本。",
			DefaultValue: defaultValue,
			Answered:     answer != "",
		})
	}
	return control.NormalizeFeishuRequestView(control.FeishuRequestView{
		RequestID:       strings.TrimSpace(flow.RequestID),
		RequestType:     "request_user_input",
		RequestRevision: flow.Revision,
		Title:           "修补当前会话",
		ThreadID:        strings.TrimSpace(flow.ThreadID),
		ThreadTitle:     strings.TrimSpace(flow.ThreadTitle),
		Sections: []control.FeishuCardTextSection{
			commandCatalogTextSection(
				"",
				"只会替换当前会话最新一轮助手回复中命中的文本。",
				"已发出的旧消息不会被回改；提交后会自动备份，并在恢复同一会话后继续使用。",
			),
		},
		Questions:            questions,
		CurrentQuestionIndex: turnPatchCurrentQuestionIndex(flow),
		StatusText:           strings.TrimSpace(flow.StatusText),
	})
}

func turnPatchPageEvent(surfaceID string, view control.FeishuPageView, inlineReplace bool) eventcontract.Event {
	return surfacePagePayloadEvent(
		surfaceID,
		eventcontract.PagePayload{View: control.NormalizeFeishuPageView(view)},
		inlineReplace,
	)
}

func turnPatchApplyingPageView(flow *turnpatchruntime.FlowRecord, inline bool) control.FeishuPageView {
	return turnPatchStatePageView(
		flow,
		"正在修补当前会话",
		"progress",
		!inline,
		false,
		nil,
		"正在备份当前会话，并重启本地 Codex 以重新加载修补后的上下文。",
		"高风险事务期间不会排队新的状态改写类输入。",
	)
}

func turnPatchAppliedPageView(flow *turnpatchruntime.FlowRecord) control.FeishuPageView {
	lines := []string{
		fmt.Sprintf("已替换 %d 处文本。", flow.ReplacedCount),
		"后续输入会基于修补后的内容继续。",
	}
	if flow.RemovedReasoning > 0 {
		lines = append(lines, fmt.Sprintf("已同步清理 %d 段 reasoning 片段。", flow.RemovedReasoning))
	}
	buttons := []control.CommandCatalogButton{}
	if strings.TrimSpace(flow.PatchID) != "" {
		buttons = append(buttons, control.CommandCatalogButton{
			Label: "回滚最近一次修补",
			Kind:  control.CommandCatalogButtonCallbackAction,
			CallbackValue: frontstagecontract.ActionPayloadPageAction(
				string(control.ActionTurnPatchRollback),
				strings.TrimSpace(flow.PatchID),
			),
			Style: "default",
		})
	}
	return turnPatchStatePageView(
		flow,
		"当前会话已修补",
		"success",
		true,
		false,
		buttons,
		lines...,
	)
}

func turnPatchRollbackRunningPageView(flow *turnpatchruntime.FlowRecord, inline bool) control.FeishuPageView {
	return turnPatchStatePageView(
		flow,
		"正在回滚最近一次修补",
		"progress",
		!inline,
		false,
		nil,
		"正在恢复修补前的备份，并重启本地 Codex 以重新加载原始上下文。",
	)
}

func turnPatchRolledBackPageView(flow *turnpatchruntime.FlowRecord) control.FeishuPageView {
	return turnPatchStatePageView(
		flow,
		"当前会话已回滚",
		"success",
		true,
		true,
		nil,
		"已恢复到修补前状态。",
		"后续输入会基于回滚后的内容继续。",
	)
}

func turnPatchFailedPageView(flow *turnpatchruntime.FlowRecord, title string, lines ...string) control.FeishuPageView {
	return turnPatchStatePageView(
		flow,
		title,
		"error",
		true,
		true,
		nil,
		lines...,
	)
}

func turnPatchCancelledPageView(flow *turnpatchruntime.FlowRecord) control.FeishuPageView {
	return turnPatchStatePageView(
		flow,
		"当前会话修补已取消",
		"",
		false,
		true,
		nil,
		"这次修补没有执行任何持久化改动。",
	)
}

func turnPatchStatePageView(flow *turnpatchruntime.FlowRecord, title, theme string, patchable, sealed bool, buttons []control.CommandCatalogButton, lines ...string) control.FeishuPageView {
	body := []control.FeishuCardTextSection{}
	trackingKey := ""
	if flow != nil {
		threadTitle := strings.TrimSpace(flow.ThreadTitle)
		if threadTitle == "" {
			threadTitle = strings.TrimSpace(flow.ThreadID)
		}
		body = append(body, commandCatalogTextSection("当前会话", threadTitle))
		body = append(body, commandCatalogTextSection("目标范围", fmt.Sprintf("最新一轮助手回复 · %d 个候选点", len(flow.Candidates))))
		if strings.TrimSpace(flow.MessageID) == "" {
			trackingKey = strings.TrimSpace(flow.FlowID)
		}
	}
	view := control.FeishuPageView{
		CommandID:      control.FeishuCommandPatch,
		Title:          strings.TrimSpace(title),
		ThemeKey:       strings.TrimSpace(theme),
		MessageID:      turnPatchMessageID(flow),
		TrackingKey:    trackingKey,
		Patchable:      patchable,
		BodySections:   body,
		NoticeSections: commandCatalogSummarySections(lines...),
		Interactive:    len(buttons) != 0 && !sealed,
		RelatedButtons: buttons,
		Sealed:         sealed,
	}
	if !sealed && strings.TrimSpace(theme) == "progress" {
		view.Phase = frontstagecontract.PhaseProcessing
		view.ActionPolicy = frontstagecontract.ActionPolicyReadOnly
	}
	return control.NormalizeFeishuPageView(view)
}

func turnPatchCurrentQuestionIndex(flow *turnpatchruntime.FlowRecord) int {
	if flow == nil || len(flow.Candidates) == 0 {
		return 0
	}
	if flow.CurrentQuestionIndex >= 0 && flow.CurrentQuestionIndex < len(flow.Candidates) {
		current := flow.Candidates[flow.CurrentQuestionIndex]
		if strings.TrimSpace(flow.Answers[current.QuestionID]) == "" {
			return flow.CurrentQuestionIndex
		}
	}
	for idx, candidate := range flow.Candidates {
		if strings.TrimSpace(flow.Answers[candidate.QuestionID]) == "" {
			return idx
		}
	}
	return len(flow.Candidates) - 1
}

func turnPatchQuestionsComplete(flow *turnpatchruntime.FlowRecord) bool {
	if flow == nil || len(flow.Candidates) == 0 {
		return false
	}
	for _, candidate := range flow.Candidates {
		if strings.TrimSpace(flow.Answers[candidate.QuestionID]) == "" {
			return false
		}
	}
	return true
}

func turnPatchMessageID(flow *turnpatchruntime.FlowRecord) string {
	if flow == nil {
		return ""
	}
	return strings.TrimSpace(flow.MessageID)
}

func turnPatchCompactText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func turnPatchPrefixRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func turnPatchLooksCJK(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
