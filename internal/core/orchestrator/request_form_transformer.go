package orchestrator

import (
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// RequestFormState contains presentation and editable data only. Authority,
// request identity, native binding, delivery, and expiry belong to the caller's
// durable request record; none can be fabricated through this form transformer.
type RequestFormState struct {
	Backend                agentproto.Backend                       `json:"backend"`
	Prompt                 *agentproto.RequestPrompt                `json:"prompt"`
	Metadata               map[string]any                           `json:"metadata"`
	WorkspaceRoot          string                                   `json:"workspace_root"`
	RequestType            string                                   `json:"request_type"`
	SemanticKind           string                                   `json:"semantic_kind"`
	Title                  string                                   `json:"title"`
	Sections               []state.RequestPromptTextSectionRecord   `json:"sections"`
	Options                []state.RequestPromptOptionRecord        `json:"options"`
	Questions              []state.RequestPromptQuestionRecord      `json:"questions"`
	StructuredForm         *state.RequestPromptStructuredFormRecord `json:"structured_form,omitempty"`
	DraftAnswers           map[string]string                        `json:"draft_answers"`
	StructuredDraftAnswers map[string][]string                      `json:"structured_draft_answers"`
	SkippedQuestionIDs     map[string]bool                          `json:"skipped_question_ids"`
	CurrentQuestionIndex   int                                      `json:"current_question_index"`
	HintText               string                                   `json:"hint_text"`
	Revision               int64                                    `json:"revision"`
	FeedbackMode           string                                   `json:"feedback_mode,omitempty"`
	Submitted              bool                                     `json:"submitted"`
}
type HumanRequestFormAction struct {
	OptionID       string              `json:"option_id,omitempty"`
	Answers        map[string][]string `json:"answers,omitempty"`
	Feedback       string              `json:"feedback,omitempty"`
	SkipQuestionID string              `json:"skip_question_id,omitempty"`
}
type RequestFeedbackCapture struct {
	Mode     string `json:"mode"`
	HintText string `json:"hint_text"`
}
type RequestFormTransition struct {
	NextForm           RequestFormState        `json:"next_form"`
	Response           map[string]any          `json:"response,omitempty"`
	Complete           bool                    `json:"complete"`
	NeedsRefresh       bool                    `json:"needs_refresh"`
	Capture            *RequestFeedbackCapture `json:"capture,omitempty"`
	FollowupHumanInput string                  `json:"followup_human_input,omitempty"`
}
type RequestFormValidationError struct{ Message string }

func (e *RequestFormValidationError) Error() string { return e.Message }
func formInvalid(message string) error              { return &RequestFormValidationError{Message: message} }
func cloneFormValue[T any](v T) (T, error) {
	var copy T
	raw, err := json.Marshal(v)
	if err != nil {
		return copy, err
	}
	err = json.Unmarshal(raw, &copy)
	return copy, err
}

func NewRequestForm(backend agentproto.Backend, prompt *agentproto.RequestPrompt, metadata map[string]any, workspaceRoot string) (RequestFormState, error) {
	var form RequestFormState
	cloned, err := cloneFormValue(prompt)
	if err != nil {
		return form, err
	}
	meta, err := cloneFormValue(metadata)
	if err != nil {
		return form, err
	}
	if meta == nil {
		meta = map[string]any{}
	}
	if cloned == nil {
		cloned = &agentproto.RequestPrompt{}
	}
	if len(cloned.Questions) > 0 && meta["questions"] == nil {
		questions := make([]any, 0, len(cloned.Questions))
		for _, q := range cloned.Questions {
			options := make([]any, 0, len(q.Options))
			for _, option := range q.Options {
				options = append(options, map[string]any{"label": option.Label, "description": option.Description})
			}
			questions = append(questions, map[string]any{"id": q.ID, "header": q.Header, "question": q.Question, "isOther": q.AllowOther, "isSecret": q.Secret, "options": options, "placeholder": q.Placeholder, "defaultValue": q.DefaultValue, "directResponse": q.DirectResponse})
		}
		meta["questions"] = questions
	}
	if len(cloned.Options) > 0 && meta["options"] == nil {
		options := make([]any, 0, len(cloned.Options))
		for _, o := range cloned.Options {
			options = append(options, map[string]any{"optionId": o.OptionID, "label": o.Label, "style": o.Style})
		}
		meta["options"] = options
	}

	definition, invalid := buildRequestPromptPresentationDefinition(backend, cloned, meta)
	if invalid != "" {
		return form, formInvalid(invalid)
	}
	if !requestPromptRenderable(definition.RequestType) {
		return form, formInvalid("当前请求类型不能转换为交互表单。")
	}
	// Pure response helpers consume the typed prompt. Preserve metadata-only
	// protocol inputs by normalizing their supported fields into this cloned data.
	if definition.RequestType == "permissions_request_approval" && cloned.Permissions == nil {
		cloned.Permissions = &agentproto.PermissionsRequestPrompt{Permissions: promptPermissionsList(nil, meta)}
	}
	if definition.RequestType == "mcp_server_elicitation" && cloned.MCPElicitation == nil {
		cloned.MCPElicitation = &agentproto.MCPElicitationPrompt{RequestedSchema: promptMCPElicitationSchema(nil, meta), Meta: promptMCPElicitationMeta(nil, meta)}
	}
	form = RequestFormState{Backend: backend, Prompt: cloned, Metadata: meta, WorkspaceRoot: workspaceRoot, RequestType: definition.RequestType, SemanticKind: definition.SemanticKind, Title: definition.Title, Sections: definition.Sections, Options: definition.Options, Questions: definition.Questions, HintText: definition.HintText, DraftAnswers: map[string]string{}, StructuredDraftAnswers: map[string][]string{}, SkippedQuestionIDs: map[string]bool{}, Revision: 1}
	return form, nil
}
func (form RequestFormState) record() *state.RequestPromptRecord {
	return &state.RequestPromptRecord{Backend: form.Backend, Prompt: form.Prompt, RequestType: form.RequestType, SemanticKind: form.SemanticKind, Title: form.Title, Sections: form.Sections, Options: form.Options, Questions: form.Questions, StructuredForm: form.StructuredForm, DraftAnswers: form.DraftAnswers, StructuredDraftAnswers: form.StructuredDraftAnswers, SkippedQuestionIDs: form.SkippedQuestionIDs, CurrentQuestionIndex: form.CurrentQuestionIndex, HintText: form.HintText}
}
func (form *RequestFormState) update(record *state.RequestPromptRecord) {
	form.Sections = record.Sections
	form.StructuredForm = record.StructuredForm
	form.DraftAnswers = record.DraftAnswers
	form.StructuredDraftAnswers = record.StructuredDraftAnswers
	form.SkippedQuestionIDs = record.SkippedQuestionIDs
	form.CurrentQuestionIndex = record.CurrentQuestionIndex
	form.HintText = record.HintText
	form.Revision++
}
