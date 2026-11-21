package workui

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// RequestFormCard renders data only. The durable UIView owns freshness and the
// native request owns authorization; emitted callbacks carry only that view ID.
// Large fields/options are rejected explicitly rather than silently truncated.
func RequestFormCard(viewID string, form orchestrator.RequestFormState) (json.RawMessage, error) {
	if viewID == "" || form.Revision < 1 {
		return nil, fmt.Errorf("request form requires a durable view ID and valid form revision")
	}
	title := form.Title
	if strings.TrimSpace(title) == "" {
		title = "需要处理请求"
	}
	elements := []map[string]any{}
	text := func(value string) {
		if block := cardkit.PlainTextBlockElement(value); block != nil {
			elements = append(elements, block)
		}
	}
	for _, section := range form.Sections {
		text(section.Label)
		text(strings.Join(section.Lines, "\n"))
	}
	if form.Submitted {
		text("已登记响应，等待原请求处理。")
	} else if form.FeedbackMode != "" {
		text("请填写对当前请求的处理意见。")
		input := map[string]any{"tag": "input", "name": "__feedback", "input_type": "multiline_text", "placeholder": cardkit.PlainText("处理意见"), "width": "fill"}
		elements = append(elements, requestFormContainer(viewID, []map[string]any{input}, "提交反馈"))
	} else if form.StructuredForm != nil {
		fields := []map[string]any{}
		for _, field := range form.StructuredForm.Fields {
			if field.Label != "" {
				fields = append(fields, cardkit.PlainTextBlockElement(field.Label))
			}
			element, err := requestStructuredInput(field, form.StructuredDraftAnswers[field.Name])
			if err != nil {
				return nil, err
			}
			fields = append(fields, element)
		}
		elements = append(elements, requestFormContainer(viewID, fields, form.StructuredForm.SubmitLabel))
		for _, option := range form.Options {
			elements = append(elements, requestFormButton(viewID, option.Label, option.Style, map[string]any{"option_id": option.OptionID}))
		}
	} else if len(form.Questions) > 0 {
		index := form.CurrentQuestionIndex
		if index < 0 || index >= len(form.Questions) {
			return nil, fmt.Errorf("request question cursor is out of range")
		}
		q := form.Questions[index]
		if q.ID == "" {
			return nil, fmt.Errorf("request question has no field identity")
		}
		text(fmt.Sprintf("问题 %d/%d", index+1, len(form.Questions)))
		text(q.Header)
		if q.Question != q.Header {
			text(q.Question)
		}
		for _, option := range q.Options {
			if option.Description != "" || q.AllowOther || q.Secret {
				line := option.Label
				if option.Description != "" {
					line += "：" + option.Description
				}
				text(line)
			}
		}
		input, err := requestQuestionInput(form, q)
		if err != nil {
			return nil, err
		}
		elements = append(elements, requestFormContainer(viewID, []map[string]any{input}, "提交"))
		if q.Secret {
			text("该答案按私密输入处理，不在共享卡片中回显。")
		}
		buttons := []map[string]any{}
		if index > 0 {
			buttons = append(buttons, requestFormButton(viewID, "上一题", "default", map[string]any{"option_id": frontstagecontract.RequestPromptOptionStepPrevious}))
		}
		if index < len(form.Questions)-1 {
			buttons = append(buttons, requestFormButton(viewID, "下一题", "default", map[string]any{"option_id": frontstagecontract.RequestPromptOptionStepNext}))
		}
		if q.Optional {
			buttons = append(buttons, requestFormButton(viewID, "跳过此题", "default", map[string]any{"skip_question_id": q.ID}))
		}
		if row := cardkit.ButtonGroupElement(buttons); row != nil {
			elements = append(elements, row)
		}
		for _, option := range form.Options {
			elements = append(elements, requestFormButton(viewID, option.Label, option.Style, map[string]any{"option_id": option.OptionID}))
		}
	} else {
		buttons := []map[string]any{}
		for _, option := range form.Options {
			buttons = append(buttons, requestFormButton(viewID, option.Label, option.Style, map[string]any{"option_id": option.OptionID}))
		}
		if row := cardkit.ButtonGroupElement(buttons); row != nil {
			elements = append(elements, row)
		}
		if len(buttons) == 0 {
			return nil, fmt.Errorf("request form has no supported interaction; use the native request interface")
		}
	}
	text(form.HintText)
	card := map[string]any{"schema": "2.0", "config": map[string]any{"update_multi": true}, "header": map[string]any{"title": cardkit.PlainText(title)}, "body": map[string]any{"elements": elements}}
	if err := validateRequestCard(card); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return nil, err
	}
	if len(raw) > 28*1024 {
		return nil, fmt.Errorf("request form exceeds the 28 KiB card budget; split the source content or use the full native request interface")
	}
	return raw, nil
}
func requestFormButton(viewID, label, style string, extra map[string]any) map[string]any {
	if strings.TrimSpace(label) == "" {
		label = "提交"
	}
	if style != "primary" && style != "danger" {
		style = "default"
	}
	value := map[string]any{"team_view": viewID, "action": "native.respond"}
	for k, v := range extra {
		value[k] = v
	}
	return map[string]any{"tag": "button", "type": style, "text": cardkit.PlainText(label), "behaviors": []map[string]any{{"type": "callback", "value": value}}}
}
func requestFormContainer(viewID string, fields []map[string]any, label string) map[string]any {
	submit := requestFormButton(viewID, label, "primary", nil)
	submit["name"] = "__native_submit"
	submit["form_action_type"] = "submit"
	return map[string]any{"tag": "form", "name": "__native_request_form", "elements": append(fields, submit)}
}
func requestQuestionInput(form orchestrator.RequestFormState, q state.RequestPromptQuestionRecord) (map[string]any, error) {
	if len(q.Options) > 100 {
		return nil, fmt.Errorf("question %s has too many choices for one selector; use a paginated or native request interface", q.ID)
	}
	if !q.Secret && len(q.Options) > 0 && !q.AllowOther {
		options := []map[string]any{}
		allowed := map[string]bool{}
		for _, o := range q.Options {
			if o.Label == "" || allowed[o.Label] {
				return nil, fmt.Errorf("question %s has empty or duplicate choice identity", q.ID)
			}
			allowed[o.Label] = true
			options = append(options, map[string]any{"text": cardkit.PlainText(o.Label), "value": o.Label})
		}
		field := map[string]any{"tag": "select_static", "name": q.ID, "options": options, "placeholder": cardkit.PlainText("请选择")}
		value := form.DraftAnswers[q.ID]
		if value == "" {
			value = q.DefaultValue
		}
		if value != "" {
			if !allowed[value] {
				return nil, fmt.Errorf("question %s default is not in the current choices", q.ID)
			}
			field["initial_option"] = value
		}
		return field, nil
	}
	field := map[string]any{"tag": "input", "name": q.ID, "input_type": "multiline_text", "width": "fill"}
	if q.Secret {
		field["input_type"] = "password"
	} else {
		value := form.DraftAnswers[q.ID]
		if value == "" {
			value = q.DefaultValue
		}
		if utf8.RuneCountInString(value) > 1000 {
			return nil, fmt.Errorf("question %s draft exceeds the card input limit; use the full native request interface", q.ID)
		}
		if value != "" {
			field["default_value"] = value
		}
	}
	if utf8.RuneCountInString(q.Placeholder) > 100 {
		return nil, fmt.Errorf("question %s placeholder exceeds the card input limit", q.ID)
	}
	if q.Placeholder != "" {
		field["placeholder"] = cardkit.PlainText(q.Placeholder)
	}
	return field, nil
}
func requestStructuredInput(field state.RequestPromptFormFieldRecord, draft []string) (map[string]any, error) {
	if utf8.RuneCountInString(field.Placeholder) > 100 {
		return nil, fmt.Errorf("structured field %s placeholder exceeds the card limit", field.Name)
	}
	if field.Name == "" {
		return nil, fmt.Errorf("structured request field identity required")
	}
	if len(field.Options) > 100 {
		return nil, fmt.Errorf("field %s has too many choices; use a paginated or native request interface", field.Name)
	}
	options := []map[string]any{}
	allowed := map[string]bool{}
	for _, o := range field.Options {
		if o.Value == "" || allowed[o.Value] {
			return nil, fmt.Errorf("structured field %s has empty or duplicate option identity", field.Name)
		}
		allowed[o.Value] = true
		options = append(options, map[string]any{"text": cardkit.PlainText(o.Label), "value": o.Value})
	}
	values := draft
	if len(values) == 0 {
		values = field.DefaultValues
	}
	if len(values) == 0 && field.DefaultValue != "" {
		values = []string{field.DefaultValue}
	}
	for _, v := range values {
		if !allowed[v] {
			return nil, fmt.Errorf("field %s default does not belong to its options", field.Name)
		}
	}
	element := map[string]any{"tag": string(field.Kind), "name": field.Name, "options": options, "placeholder": cardkit.PlainText(field.Placeholder)}
	if field.Placeholder == "" {
		element["placeholder"] = cardkit.PlainText("请选择")
	}
	switch field.Kind {
	case state.RequestPromptFormFieldSelectStatic:
		if len(values) > 1 {
			return nil, fmt.Errorf("field %s is single-select", field.Name)
		}
		if len(values) == 1 {
			element["initial_option"] = values[0]
		}
	case state.RequestPromptFormFieldMultiSelectStatic:
		if len(values) > 0 {
			element["selected_values"] = values
		}
	default:
		return nil, fmt.Errorf("unsupported structured request field type %q", field.Kind)
	}
	return element, nil
}
func validateRequestCard(card map[string]any) error {
	count := 0
	names := map[string]bool{}
	var failure error
	var walk func(any, int)
	walk = func(value any, depth int) {
		if failure != nil {
			return
		}
		switch v := value.(type) {
		case map[string]any:
			if _, ok := v["tag"]; ok {
				count++
				if depth > 5 {
					failure = fmt.Errorf("request form exceeds card nesting budget")
					return
				}
			}
			if name, ok := v["name"].(string); ok {
				if names[name] || name == "" {
					failure = fmt.Errorf("request form contains duplicate/empty component name %q", name)
					return
				}
				names[name] = true
			}
			for k, child := range v {
				next := depth
				if k == "elements" || k == "columns" {
					next++
				}
				walk(child, next)
			}
		case []map[string]any:
			for _, child := range v {
				walk(child, depth)
			}
		case []any:
			for _, child := range v {
				walk(child, depth)
			}
		}
	}
	walk(card, 0)
	if failure != nil {
		return failure
	}
	if count > 190 {
		return fmt.Errorf("request form exceeds the card component budget; paginate the source instead of truncating it")
	}
	return nil
}
