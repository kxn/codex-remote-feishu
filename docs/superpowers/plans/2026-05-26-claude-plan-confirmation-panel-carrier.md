# Claude Plan Confirmation Panel Carrier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the request-local structured form and multi-select carrier needed for the Feishu `plan_confirmation` complex permission panel in `#665`, without mixing in Claude native `updatedPermissions` mapping.

**Architecture:** Keep the existing `Questions` path intact for `request_user_input` and MCP forms, and add a separate request-local structured form carrier for approval-family cards. Model the permission panel as a `plan_confirmation` request subphase that stays inside the pending-request lifecycle, then render and submit it through Feishu form controls with full multi-select preservation.

**Tech Stack:** Go, GitHub issue workflow, Feishu card JSON v2, current orchestrator/request state machine, focused package tests.

---

### Task 1: Add Request Structured Form Carrier Types

**Files:**
- Modify: `internal/core/control/types.go`
- Modify: `internal/core/control/feishu_request_view.go`
- Modify: `internal/core/state/types.go`
- Modify: `internal/core/orchestrator/service_request.go`
- Test: `internal/core/orchestrator/service_request_structured_form_test.go`

- [ ] **Step 1: Write the failing state/view test**

```go
func TestRequestPromptViewIncludesStructuredFormAndMultiValueDrafts(t *testing.T) {
	svc := newServiceForTest(nil)
	record := &state.RequestPromptRecord{
		RequestID:    "req-1",
		RequestType:  "approval",
		SemanticKind: control.RequestSemanticPlanConfirmation,
		Title:        "需要确认计划",
		StructuredForm: &state.RequestPromptStructuredFormRecord{
			SubmitLabel: "按以上授权继续",
			Fields: []state.RequestPromptFormFieldRecord{
				{
					Name:  "grant_level",
					Kind:  "select_static",
					Label: "授权级别",
					Options: []state.RequestPromptFormFieldOptionRecord{
						{Label: "仅按选中范围自动允许", Value: "scoped_rules"},
					},
					DefaultValues: []string{"scoped_rules"},
				},
				{
					Name:  "directories",
					Kind:  "multi_select_static",
					Label: "目录范围",
					Options: []state.RequestPromptFormFieldOptionRecord{
						{Label: "internal/core/orchestrator", Value: "internal/core/orchestrator"},
						{Label: "internal/adapter/feishu", Value: "internal/adapter/feishu"},
					},
					DefaultValues: []string{"internal/core/orchestrator", "internal/adapter/feishu"},
				},
			},
		},
		StructuredDraftAnswers: map[string][]string{
			"grant_level": {"scoped_rules"},
			"directories": {"internal/core/orchestrator", "internal/adapter/feishu"},
		},
	}

	view := svc.requestPromptView(record, "")
	if view.StructuredForm == nil {
		t.Fatalf("expected structured form in request view")
	}
	if got := view.StructuredForm.Fields[1].DefaultValues; len(got) != 2 {
		t.Fatalf("expected multi-value defaults, got %#v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/orchestrator -run TestRequestPromptViewIncludesStructuredFormAndMultiValueDrafts -count=1`
Expected: FAIL with missing `StructuredForm` / `StructuredDraftAnswers` types or fields.

- [ ] **Step 3: Add the carrier types with explicit multi-value support**

```go
type RequestPromptFormFieldKind string

const (
	RequestPromptFormFieldText              RequestPromptFormFieldKind = "text"
	RequestPromptFormFieldSelectStatic      RequestPromptFormFieldKind = "select_static"
	RequestPromptFormFieldMultiSelectStatic RequestPromptFormFieldKind = "multi_select_static"
)

type RequestPromptFormFieldOption struct {
	Label string
	Value string
}

type RequestPromptFormField struct {
	Name          string
	Kind          RequestPromptFormFieldKind
	Label         string
	Placeholder   string
	DefaultValue  string
	DefaultValues []string
	Options       []RequestPromptFormFieldOption
}

type RequestPromptStructuredForm struct {
	SubmitLabel string
	Fields      []RequestPromptFormField
}
```

```go
type RequestPromptRecord struct {
	// existing fields...
	StructuredForm         *RequestPromptStructuredFormRecord
	StructuredDraftAnswers map[string][]string
}
```

```go
type FeishuRequestView struct {
	// existing fields...
	StructuredForm *RequestPromptStructuredForm
}
```

- [ ] **Step 4: Wire the request view projection without disturbing the old Questions path**

```go
view := control.FeishuRequestView{
	RequestID:       record.RequestID,
	RequestType:     record.RequestType,
	SemanticKind:    requestPromptSemanticKind(record),
	Title:           record.Title,
	Sections:        requestPromptSectionsToControl(record.Sections),
	Options:         requestPromptOptionsToControl(record.Options),
	Questions:       requestPromptQuestionsToControl(record.Questions, record.DraftAnswers, record.SkippedQuestionIDs),
	StructuredForm:  requestPromptStructuredFormToControl(record.StructuredForm, record.StructuredDraftAnswers),
	CurrentQuestionIndex: normalizedRequestPromptCurrentQuestionIndex(record),
	HintText:        strings.TrimSpace(record.HintText),
	Phase:           record.Phase,
}
```

- [ ] **Step 5: Run the focused test**

Run: `go test ./internal/core/orchestrator -run TestRequestPromptViewIncludesStructuredFormAndMultiValueDrafts -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/core/control/types.go internal/core/control/feishu_request_view.go internal/core/state/types.go internal/core/orchestrator/service_request.go internal/core/orchestrator/service_request_structured_form_test.go
git commit -m "feat: add request structured form carrier"
```

### Task 2: Render And Parse Request Structured Form Fields

**Files:**
- Modify: `internal/adapter/feishu/projector/request.go`
- Modify: `internal/adapter/feishu/gateway/routing.go`
- Modify: `internal/adapter/feishu/selectflow/contract.go`
- Test: `internal/adapter/feishu/projector_request_structured_form_test.go`
- Test: `internal/adapter/feishu/gateway_test.go`

- [ ] **Step 1: Write the failing projector test for `multi_select_static`**

```go
func TestRequestPromptStructuredFormRendersMultiSelectStatic(t *testing.T) {
	prompt := control.FeishuRequestView{
		RequestID:       "req-1",
		RequestType:     "approval",
		SemanticKind:    control.RequestSemanticPlanConfirmation,
		RequestRevision: 3,
		StructuredForm: &control.RequestPromptStructuredForm{
			SubmitLabel: "按以上授权继续",
			Fields: []control.RequestPromptFormField{
				{
					Name:  "directories",
					Kind:  control.RequestPromptFormFieldMultiSelectStatic,
					Label: "目录范围",
					Options: []control.RequestPromptFormFieldOption{
						{Label: "internal/core/orchestrator", Value: "internal/core/orchestrator"},
						{Label: "internal/adapter/feishu", Value: "internal/adapter/feishu"},
					},
					DefaultValues: []string{"internal/core/orchestrator"},
				},
			},
		},
	}

	elements := RequestPromptElements(prompt, "daemon-1")
	form := findCardFormByName(t, elements, "request_form_req-1_structured")
	field := firstFormFieldByName(t, form, "directories")
	if got := cardStringValue(field["tag"]); got != "multi_select_static" {
		t.Fatalf("expected multi_select_static, got %#v", field)
	}
}
```

- [ ] **Step 2: Write the failing gateway test for preserving multi-select values**

```go
func TestParseCardActionTriggerEventPreservesRequestFormMultiSelectValues(t *testing.T) {
	action := fakeRequestSubmitCardActionEvent(t, map[string]any{
		"kind":             "submit_request_form",
		"request_id":       "req-1",
		"request_type":     "approval",
		"request_revision": 5,
	}, map[string]any{
		"directories": []any{"internal/core/orchestrator", "internal/adapter/feishu"},
		"grant_level": "scoped_rules",
	})

	parsed, ok := gateway.ParseCardActionTriggerEvent(testRoutingEnv(), action)
	if !ok {
		t.Fatal("expected request form action to parse")
	}
	if got := parsed.Request.Answers["directories"]; len(got) != 2 {
		t.Fatalf("expected multi-select values preserved, got %#v", parsed.Request.Answers)
	}
}
```

- [ ] **Step 3: Run both tests to verify they fail**

Run:
- `go test ./internal/adapter/feishu -run TestRequestPromptStructuredFormRendersMultiSelectStatic -count=1`
- `go test ./internal/adapter/feishu -run TestParseCardActionTriggerEventPreservesRequestFormMultiSelectValues -count=1`

Expected: FAIL because request structured form rendering and multi-select parsing do not exist yet.

- [ ] **Step 4: Render structured form fields in the request projector**

```go
func requestPromptStructuredFormElement(prompt control.FeishuRequestView, daemonLifecycleID string) map[string]any {
	if prompt.StructuredForm == nil || prompt.Sealed {
		return nil
	}
	elements := make([]map[string]any, 0, len(prompt.StructuredForm.Fields)+1)
	for _, field := range prompt.StructuredForm.Fields {
		elements = append(elements, requestStructuredFormFieldElement(field))
	}
	elements = append(elements, cardFormActionButtonElement(
		firstNonEmpty(strings.TrimSpace(prompt.StructuredForm.SubmitLabel), "提交"),
		"primary",
		stampActionValue(map[string]any{
			cardActionPayloadKeyKind:            cardActionKindSubmitRequestForm,
			cardActionPayloadKeyRequestID:       prompt.RequestID,
			cardActionPayloadKeyRequestType:     prompt.RequestType,
			cardActionPayloadKeyRequestRevision: prompt.RequestRevision,
		}, daemonLifecycleID),
		false,
		"fill",
	))
	return map[string]any{
		"tag":      "form",
		"name":     "request_form_" + strings.TrimSpace(prompt.RequestID) + "_structured",
		"elements": elements,
	}
}
```

```go
switch field.Kind {
case control.RequestPromptFormFieldMultiSelectStatic:
	element["tag"] = "multi_select_static"
	element["max_selected_items"] = len(field.Options)
	element["initial_options"] = append([]string(nil), field.DefaultValues...)
case control.RequestPromptFormFieldSelectStatic:
	element["tag"] = "select_static"
	element["initial_option"] = firstNonEmpty(field.DefaultValue, firstDefault(field.DefaultValues))
default:
	element["tag"] = "input"
}
```

- [ ] **Step 5: Preserve multi-select answers in gateway helpers**

```go
func requestAnswersFromFormValue(values map[string]interface{}) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	return requestAnswersFromMap(values)
}
```

```go
func SelectedOptionValues(action *larkcallback.CallBackAction) []string {
	if action == nil {
		return nil
	}
	if len(action.Options) != 0 {
		return append([]string(nil), action.Options...)
	}
	if option := strings.TrimSpace(action.Option); option != "" {
		return []string{option}
	}
	return nil
}
```

- [ ] **Step 6: Run the focused Feishu tests**

Run:
- `go test ./internal/adapter/feishu -run TestRequestPromptStructuredFormRendersMultiSelectStatic -count=1`
- `go test ./internal/adapter/feishu -run TestParseCardActionTriggerEventPreservesRequestFormMultiSelectValues -count=1`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/feishu/projector/request.go internal/adapter/feishu/gateway/routing.go internal/adapter/feishu/selectflow/contract.go internal/adapter/feishu/projector_request_structured_form_test.go internal/adapter/feishu/gateway_test.go
git commit -m "feat: add request structured form rendering"
```

### Task 3: Implement `plan_confirmation` Quick-Decision And Permission-Configuring Subphases

**Files:**
- Modify: `internal/core/orchestrator/service_request.go`
- Modify: `internal/core/orchestrator/service_request_presentation.go`
- Modify: `internal/core/orchestrator/service_helpers_request.go`
- Modify: `internal/core/state/types.go`
- Test: `internal/core/orchestrator/service_request_plan_permission_panel_test.go`

- [ ] **Step 1: Write the failing orchestrator transition test**

```go
func TestPlanConfirmationConfigurePermissionsEntersStructuredPanel(t *testing.T) {
	svc := newServiceForTest(nil)
	surface := attachClaudeRequestTestSurface(svc)
	record := pendingPlanConfirmationRequestWithOptions("req-plan-1")
	surface.PendingRequests = map[string]*state.RequestPromptRecord{"req-plan-1": record}
	surface.PendingRequestOrder = []string{"req-plan-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: surface.SurfaceSessionID,
		Request:          testRequestAction("req-plan-1", "approval", "configurePermissions", nil, record.CardRevision),
	})

	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected inline refresh, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.StructuredForm == nil || prompt.StructuredForm.SubmitLabel != "按以上授权继续" {
		t.Fatalf("expected permission panel structured form, got %#v", prompt)
	}
}
```

- [ ] **Step 2: Write the failing summary-seal test**

```go
func TestPlanConfirmationStructuredFormSubmitSealsSummaryBeforeDispatch(t *testing.T) {
	svc := newServiceForTest(nil)
	surface := attachClaudeRequestTestSurface(svc)
	record := pendingPlanConfirmationPermissionPanel("req-plan-1")
	surface.PendingRequests = map[string]*state.RequestPromptRecord{"req-plan-1": record}
	surface.PendingRequestOrder = []string{"req-plan-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: surface.SurfaceSessionID,
		Request: testRequestAction("req-plan-1", "approval", "", map[string][]string{
			"grant_level": {"scoped_rules"},
			"directories": {"internal/core/orchestrator", "internal/adapter/feishu"},
			"rule_classes": {"edit_existing_files", "create_new_files"},
		}, record.CardRevision),
		RequestAnswers: map[string][]string{
			"grant_level": {"scoped_rules"},
			"directories": {"internal/core/orchestrator", "internal/adapter/feishu"},
			"rule_classes": {"edit_existing_files", "create_new_files"},
		},
	})

	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected sealed summary plus dispatch command, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if !prompt.Sealed || !containsSectionLine(prompt.Sections, "有效期：当前会话") {
		t.Fatalf("expected sealed session summary, got %#v", prompt)
	}
}
```

- [ ] **Step 3: Run the focused orchestrator tests to verify failure**

Run: `go test ./internal/core/orchestrator -run 'TestPlanConfirmation(ConfigurePermissionsEntersStructuredPanel|StructuredFormSubmitSealsSummaryBeforeDispatch)' -count=1`
Expected: FAIL because plan confirmation has no permission-configuring subphase yet.

- [ ] **Step 4: Add local option handling and structured-form submit handling**

```go
if optionID == "configurePermissions" && requestPromptSemanticKind(request) == control.RequestSemanticPlanConfirmation {
	ensurePlanPermissionConfigPhase(request)
	bumpRequestCardRevision(request)
	return nil, false, []eventcontract.Event{s.requestPromptInlineEvent(surface, request, "")}
}
if optionID == "backToDecision" && requestPromptSemanticKind(request) == control.RequestSemanticPlanConfirmation {
	restorePlanPermissionDecisionPhase(request)
	bumpRequestCardRevision(request)
	return nil, false, []eventcontract.Event{s.requestPromptInlineEvent(surface, request, "")}
}
if request.StructuredForm != nil && requestPromptSemanticKind(request) == control.RequestSemanticPlanConfirmation {
	response, errText := buildPlanPermissionSelectionResponse(request, requestAnswers)
	if errText != "" {
		return nil, false, notice(surface, "request_invalid", errText)
	}
	return response, true, nil
}
```

```go
func buildPlanPermissionSelectionResponse(request *state.RequestPromptRecord, rawAnswers map[string][]string) (map[string]any, string) {
	selection := map[string]any{
		"scope": "session",
		"grant_level": firstTrimmedAnswer(rawAnswers["grant_level"]),
		"directories": copyTrimmedAnswers(rawAnswers["directories"]),
		"rule_classes": copyTrimmedAnswers(rawAnswers["rule_classes"]),
	}
	if selection["grant_level"] == "" {
		return nil, "请先选择授权级别。"
	}
	return map[string]any{
		"type": "approval",
		"decision": "accept",
		"permissionSelection": selection,
	}, ""
}
```

- [ ] **Step 5: Add the sealed summary projection**

```go
func markPlanPermissionSelectionSubmitted(request *state.RequestPromptRecord, selection map[string]any) {
	request.StructuredDraftAnswers = normalizeStructuredDraftAnswers(selection)
	request.StructuredForm = nil
	request.Sections = appendRequestPromptSection(nil, "", "已按本会话授权继续")
	request.Sections = appendRequestPromptSection(request.Sections, "授权级别", displayGrantLevel(selection))
	request.Sections = appendRequestPromptSection(request.Sections, "目录范围", displayList(selection["directories"])...)
	request.Sections = appendRequestPromptSection(request.Sections, "规则范围", displayList(selection["rule_classes"])...)
	request.Sections = appendRequestPromptSection(request.Sections, "", "有效期：当前会话")
	request.Phase = frontstagecontract.PhaseWaitingDispatch
}
```

- [ ] **Step 6: Run the focused orchestrator tests**

Run: `go test ./internal/core/orchestrator -run 'TestPlanConfirmation(ConfigurePermissionsEntersStructuredPanel|StructuredFormSubmitSealsSummaryBeforeDispatch)' -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/core/orchestrator/service_request.go internal/core/orchestrator/service_request_presentation.go internal/core/orchestrator/service_helpers_request.go internal/core/state/types.go internal/core/orchestrator/service_request_plan_permission_panel_test.go
git commit -m "feat: add plan confirmation permission panel flow"
```

### Task 4: Sync Docs And Run Final Validation

**Files:**
- Modify: `docs/general/relay-protocol-spec.md`
- Modify: `docs/general/feishu-card-ui-state-machine.md`
- Modify: `docs/general/remote-surface-state-machine.md`
- Modify: `#665` issue body snapshot if implementation details drift

- [ ] **Step 1: Update the protocol/state-machine docs after code is green**

```md
- `plan_confirmation` now supports a local `configurePermissions` branch.
- The permission panel remains inside the pending-request lifecycle.
- Structured form submit seals the current card into a waiting-dispatch summary before request dispatch.
- `multi_select_static` request fields preserve full selected arrays.
```

- [ ] **Step 2: Run focused package validation**

Run:
- `go test ./internal/core/orchestrator`
- `go test ./internal/adapter/feishu`
- `go test ./internal/app/daemon`
- `bash scripts/check/go-file-length.sh`

Expected: all commands PASS

- [ ] **Step 3: Refresh the issue snapshot if implementation shape changed**

```md
### 当前阶段

- close-out

### 当前执行点

- request-local structured form / multi-select carrier 已落地
- `plan_confirmation` 已支持 quick-decision -> permission-configuring -> sealed summary
```

- [ ] **Step 4: Commit**

```bash
git add docs/general/relay-protocol-spec.md docs/general/feishu-card-ui-state-machine.md docs/general/remote-surface-state-machine.md
git commit -m "docs: sync plan confirmation permission panel state"
```
