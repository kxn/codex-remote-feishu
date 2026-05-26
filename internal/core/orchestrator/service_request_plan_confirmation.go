package orchestrator

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	planConfirmationFieldGrantLevel  = "grant_level"
	planConfirmationFieldDirectories = "directories"
	planConfirmationFieldRuleClasses = "rule_classes"

	planConfirmationSelectionScopeSession = "session"

	planConfirmationGrantLevelScopedRules              = "scoped_rules"
	planConfirmationGrantLevelSessionFileEdits         = "session_file_edits"
	planConfirmationGrantLevelSessionFileEditsAndFSOps = "session_file_edits_and_fs_ops"

	planConfirmationRuleClassEditExistingFiles   = "edit_existing_files"
	planConfirmationRuleClassCreateNewFiles      = "create_new_files"
	planConfirmationRuleClassRenameOrMoveFiles   = "rename_or_move_files"
	planConfirmationRuleClassDeletePlanFiles     = "delete_plan_files"
	planConfirmationRuleClassRunCommonFSCommands = "run_common_fs_commands"
)

func planConfirmationQuickDecisionOptions(backend agentproto.Backend) []state.RequestPromptOptionRecord {
	return []state.RequestPromptOptionRecord{
		{OptionID: "accept", Label: "允许一次并执行", Style: "primary"},
		{OptionID: "acceptForSession", Label: "配置本会话授权", Style: "default"},
		{OptionID: "decline", Label: "拒绝", Style: "default"},
		{OptionID: "revise", Label: requestFeedbackActionLabel(backend), Style: "default"},
	}
}

func planConfirmationPanelOptions() []control.RequestPromptOption {
	return []control.RequestPromptOption{
		{OptionID: frontstagecontract.RequestPromptOptionStepPrevious, Label: "返回", Style: "default"},
		{OptionID: "decline", Label: "拒绝", Style: "default"},
	}
}

func planConfirmationRequestViewOverrides(record *state.RequestPromptRecord, view control.FeishuRequestView) control.FeishuRequestView {
	if record == nil || record.StructuredForm == nil || requestPromptSemanticKind(record) != control.RequestSemanticPlanConfirmation {
		return view
	}
	view.Sections = append(view.Sections, control.FeishuCardTextSection{
		Label: "授权说明",
		Lines: []string{
			"仅当前会话有效",
			"未展示或未勾选的权限默认不授予",
			"这不是全局永久授权",
		},
	}.Normalized())
	view.Options = planConfirmationPanelOptions()
	view.HintText = "请先配置本会话授权范围；只有提交面板后，当前计划才会继续执行。"
	return view
}

func (s *Service) maybeHandlePlanConfirmationRequestAction(
	surface *state.SurfaceConsoleRecord,
	request *state.RequestPromptRecord,
	action control.Action,
	optionID string,
	requestAnswers map[string][]string,
) (map[string]any, bool, []eventcontract.Event, bool) {
	if request == nil || requestPromptSemanticKind(request) != control.RequestSemanticPlanConfirmation {
		return nil, false, nil, false
	}
	if request.StructuredForm != nil {
		switch {
		case requestPromptStepPrevious(optionID):
			request.StructuredForm = nil
			bumpRequestCardRevision(request)
			return nil, false, []eventcontract.Event{s.requestPromptInlineEvent(surface, request, "")}, true
		case optionID == "":
			response, complete, errText := buildPlanConfirmationPermissionSelectionResponse(request, requestAnswers)
			if errText != "" {
				return nil, false, notice(surface, "request_invalid", errText), true
			}
			return response, complete, nil, true
		}
	}
	if optionID != "acceptForSession" {
		return nil, false, nil, false
	}
	request.StructuredForm = buildPlanConfirmationStructuredForm(request, s.planConfirmationWorkspaceRoot(request))
	bumpRequestCardRevision(request)
	return nil, false, []eventcontract.Event{s.requestPromptInlineEvent(surface, request, "")}, true
}

func (s *Service) planConfirmationWorkspaceRoot(request *state.RequestPromptRecord) string {
	if request == nil {
		return ""
	}
	inst := s.root.Instances[request.InstanceID]
	if inst == nil {
		return ""
	}
	if thread := inst.Threads[request.ThreadID]; threadVisible(thread) && strings.TrimSpace(thread.CWD) != "" {
		return strings.TrimSpace(thread.CWD)
	}
	return strings.TrimSpace(firstNonEmpty(inst.WorkspaceRoot, inst.WorkspaceKey))
}

func buildPlanConfirmationStructuredForm(request *state.RequestPromptRecord, workspaceRoot string) *state.RequestPromptStructuredFormRecord {
	directoryOptions, defaultDirectories := planConfirmationDirectoryOptions(request, workspaceRoot)
	ruleOptions, defaultRules := planConfirmationRuleOptions(request)
	defaultGrantLevel := normalizedStructuredDraftValues(request.StructuredDraftAnswers[planConfirmationFieldGrantLevel])
	if len(defaultGrantLevel) == 0 {
		defaultGrantLevel = []string{planConfirmationGrantLevelScopedRules}
	}
	if drafts := normalizedStructuredDraftValues(request.StructuredDraftAnswers[planConfirmationFieldDirectories]); len(drafts) != 0 {
		defaultDirectories = drafts
	}
	if drafts := normalizedStructuredDraftValues(request.StructuredDraftAnswers[planConfirmationFieldRuleClasses]); len(drafts) != 0 {
		defaultRules = drafts
	}
	return &state.RequestPromptStructuredFormRecord{
		SubmitLabel: "按以上授权继续",
		Fields: []state.RequestPromptFormFieldRecord{
			{
				Name:  planConfirmationFieldGrantLevel,
				Kind:  state.RequestPromptFormFieldSelectStatic,
				Label: "授权级别",
				Options: []state.RequestPromptFormFieldOptionRecord{
					{Label: "仅按下面选中的范围自动允许", Value: planConfirmationGrantLevelScopedRules},
					{Label: "本会话自动允许文件修改（更快）", Value: planConfirmationGrantLevelSessionFileEdits},
					{Label: "本会话自动允许文件修改和常见文件系统操作（更激进）", Value: planConfirmationGrantLevelSessionFileEditsAndFSOps},
				},
				DefaultValues: defaultGrantLevel,
			},
			{
				Name:          planConfirmationFieldDirectories,
				Kind:          state.RequestPromptFormFieldMultiSelectStatic,
				Label:         "目录范围",
				Options:       directoryOptions,
				DefaultValues: defaultDirectories,
			},
			{
				Name:          planConfirmationFieldRuleClasses,
				Kind:          state.RequestPromptFormFieldMultiSelectStatic,
				Label:         "规则范围",
				Options:       ruleOptions,
				DefaultValues: defaultRules,
			},
		},
	}
}

func buildPlanConfirmationPermissionSelectionResponse(request *state.RequestPromptRecord, rawAnswers map[string][]string) (map[string]any, bool, string) {
	if request == nil || request.StructuredForm == nil {
		return nil, false, "当前权限面板已经失效，请重新打开后再提交。"
	}
	answers := map[string][]string{}
	for _, field := range request.StructuredForm.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		values := normalizedStructuredDraftValues(rawAnswers[name])
		if len(values) == 0 {
			values = normalizedStructuredDraftValues(request.StructuredDraftAnswers[name])
		}
		if len(values) == 0 {
			values = normalizedStructuredDraftValues(field.DefaultValues)
		}
		if len(values) == 0 && strings.TrimSpace(field.DefaultValue) != "" {
			values = []string{strings.TrimSpace(field.DefaultValue)}
		}
		if len(values) == 0 {
			return nil, false, fmt.Sprintf("“%s”还没有配置完成。", firstNonEmpty(strings.TrimSpace(field.Label), name))
		}
		allowed := map[string]bool{}
		for _, option := range field.Options {
			allowed[strings.TrimSpace(option.Value)] = true
		}
		for _, value := range values {
			if len(allowed) != 0 && !allowed[value] {
				return nil, false, fmt.Sprintf("“%s”的选择项无效。", firstNonEmpty(strings.TrimSpace(field.Label), name))
			}
		}
		if field.Kind == state.RequestPromptFormFieldSelectStatic && len(values) > 1 {
			return nil, false, fmt.Sprintf("“%s”只能选择一项。", firstNonEmpty(strings.TrimSpace(field.Label), name))
		}
		answers[name] = values
	}
	request.StructuredDraftAnswers = answers
	request.Sections = planConfirmationSealedSummarySections(request.StructuredForm, answers)
	request.StructuredForm = nil
	request.HintText = "仅当前会话有效。"
	return map[string]any{
		"type":     "approval",
		"decision": "accept",
		"permissionSelection": map[string]any{
			"scope":        planConfirmationSelectionScopeSession,
			"grant_level":  firstTrimmedAnswer(answers[planConfirmationFieldGrantLevel]),
			"directories":  append([]string(nil), answers[planConfirmationFieldDirectories]...),
			"rule_classes": append([]string(nil), answers[planConfirmationFieldRuleClasses]...),
		},
	}, true, ""
}

func planConfirmationSealedSummarySections(form *state.RequestPromptStructuredFormRecord, answers map[string][]string) []state.RequestPromptTextSectionRecord {
	return []state.RequestPromptTextSectionRecord{state.RequestPromptTextSectionRecord{
		Label: "授权摘要",
		Lines: []string{
			"已按本会话授权继续",
			"授权级别：" + planConfirmationSelectionLabels(form, planConfirmationFieldGrantLevel, answers),
			"目录范围：" + planConfirmationSelectionLabels(form, planConfirmationFieldDirectories, answers),
			"规则范围：" + planConfirmationSelectionLabels(form, planConfirmationFieldRuleClasses, answers),
			"有效期：当前会话",
		},
	}.Normalized()}
}

func planConfirmationSelectionLabels(form *state.RequestPromptStructuredFormRecord, fieldName string, answers map[string][]string) string {
	field := planConfirmationFieldByName(form, fieldName)
	if field == nil {
		return strings.Join(answers[fieldName], "、")
	}
	labels := make([]string, 0, len(answers[fieldName]))
	for _, value := range answers[fieldName] {
		label := value
		for _, option := range field.Options {
			if strings.TrimSpace(option.Value) == strings.TrimSpace(value) {
				label = strings.TrimSpace(option.Label)
				break
			}
		}
		if strings.TrimSpace(label) != "" {
			labels = append(labels, strings.TrimSpace(label))
		}
	}
	if len(labels) == 0 {
		return "未选择"
	}
	return strings.Join(labels, "、")
}

func planConfirmationFieldByName(form *state.RequestPromptStructuredFormRecord, fieldName string) *state.RequestPromptFormFieldRecord {
	if form == nil {
		return nil
	}
	fieldName = strings.TrimSpace(fieldName)
	for i := range form.Fields {
		if strings.TrimSpace(form.Fields[i].Name) == fieldName {
			return &form.Fields[i]
		}
	}
	return nil
}

func planConfirmationDirectoryOptions(request *state.RequestPromptRecord, workspaceRoot string) ([]state.RequestPromptFormFieldOptionRecord, []string) {
	options := make([]state.RequestPromptFormFieldOptionRecord, 0, 8)
	defaults := make([]string, 0, 4)
	seen := map[string]bool{}
	add := func(label, value string, selected bool) {
		label = strings.TrimSpace(label)
		value = strings.TrimSpace(value)
		if label == "" || value == "" || seen[value] {
			return
		}
		seen[value] = true
		options = append(options, state.RequestPromptFormFieldOptionRecord{Label: label, Value: value})
		if selected {
			defaults = append(defaults, value)
		}
	}
	for _, line := range planConfirmationSourceLines(request) {
		for _, token := range strings.Fields(line) {
			label, value := planConfirmationTokenDirectory(token, workspaceRoot)
			if label == "" || value == "" {
				continue
			}
			add(label, value, true)
		}
	}
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot != "" {
		add("整个当前工作区", workspaceRoot, len(defaults) == 0)
	}
	if len(defaults) == 0 && len(options) != 0 {
		defaults = append(defaults, options[0].Value)
	}
	return options, normalizedStructuredDraftValues(defaults)
}

func planConfirmationSourceLines(request *state.RequestPromptRecord) []string {
	if request == nil {
		return nil
	}
	lines := make([]string, 0, len(request.Sections)*2)
	for _, section := range request.Sections {
		if label := strings.TrimSpace(section.Label); label != "" {
			lines = append(lines, label)
		}
		lines = append(lines, section.Lines...)
	}
	return lines
}

func planConfirmationTokenDirectory(token, workspaceRoot string) (string, string) {
	token = strings.TrimSpace(strings.Trim(token, "`'\"(),.:;[]{}"))
	if token == "" || token == "-" || strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://") {
		return "", ""
	}
	isAbs := filepath.IsAbs(token)
	if !isAbs && !strings.Contains(token, "/") && !strings.Contains(token, string(filepath.Separator)) {
		return "", ""
	}
	var absPath string
	var relLabel string
	switch {
	case isAbs:
		absPath = filepath.Clean(token)
		relLabel = filepath.ToSlash(absPath)
		if workspaceRoot != "" {
			if rel, err := filepath.Rel(workspaceRoot, absPath); err == nil && rel != "." && rel != "" && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
				relLabel = filepath.ToSlash(rel)
			}
		}
	default:
		rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(token, "./")))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", ""
		}
		relLabel = filepath.ToSlash(rel)
		absPath = rel
		if workspaceRoot != "" {
			absPath = filepath.Clean(filepath.Join(workspaceRoot, rel))
		}
	}
	if filepath.Ext(relLabel) != "" {
		relLabel = filepath.ToSlash(filepath.Dir(relLabel))
		absPath = filepath.Dir(absPath)
	}
	relLabel = strings.Trim(strings.TrimSpace(relLabel), "/")
	absPath = strings.TrimSpace(absPath)
	if relLabel == "" || relLabel == "." || absPath == "" {
		return "", ""
	}
	return relLabel, absPath
}

func planConfirmationRuleOptions(request *state.RequestPromptRecord) ([]state.RequestPromptFormFieldOptionRecord, []string) {
	options := []state.RequestPromptFormFieldOptionRecord{
		{Label: "编辑现有文件", Value: planConfirmationRuleClassEditExistingFiles},
		{Label: "创建新文件", Value: planConfirmationRuleClassCreateNewFiles},
		{Label: "重命名或移动文件", Value: planConfirmationRuleClassRenameOrMoveFiles},
		{Label: "删除计划中涉及的文件", Value: planConfirmationRuleClassDeletePlanFiles},
		{Label: "执行常见文件系统命令", Value: planConfirmationRuleClassRunCommonFSCommands},
	}
	defaults := []string{planConfirmationRuleClassEditExistingFiles}
	source := strings.ToLower(strings.Join(planConfirmationSourceLines(request), "\n"))
	if strings.Contains(source, "创建") || strings.Contains(source, "新增") || strings.Contains(source, "new file") || strings.Contains(source, "create") {
		defaults = append(defaults, planConfirmationRuleClassCreateNewFiles)
	}
	if strings.Contains(source, "重命名") || strings.Contains(source, "移动") || strings.Contains(source, "rename") || strings.Contains(source, "move") {
		defaults = append(defaults, planConfirmationRuleClassRenameOrMoveFiles)
	}
	if strings.Contains(source, "删除") || strings.Contains(source, "remove") || strings.Contains(source, "delete") {
		defaults = append(defaults, planConfirmationRuleClassDeletePlanFiles)
	}
	if strings.Contains(source, "mkdir") || strings.Contains(source, "mv ") || strings.Contains(source, "cp ") || strings.Contains(source, "touch ") {
		defaults = append(defaults, planConfirmationRuleClassRunCommonFSCommands)
	}
	return options, normalizedStructuredDraftValues(defaults)
}
