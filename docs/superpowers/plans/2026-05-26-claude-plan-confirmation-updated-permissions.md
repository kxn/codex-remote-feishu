# Claude Plan Confirmation Updated Permissions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compile `plan_confirmation`'s structured `permissionSelection` into Claude native session-scoped `updatedPermissions` without widening the grant beyond what the user selected.

**Architecture:** Keep the Feishu/orchestrator carrier from `#665` unchanged and add a Claude-translator-local compiler that turns `scope=session + grant_level + directories[] + rule_classes[]` into native `PermissionUpdate[]`. Prefer session-scoped additive rules and directory grants; use `setMode(acceptEdits)` only in the narrow case where the user selected the whole current workspace and the rule-class set already matches the full `acceptEdits` capability bundle.

**Tech Stack:** Go, Claude translator NDJSON payloads, Agent SDK permission update schema, focused translator tests, GitHub issue workflow.

---

### Task 1: Lock The `permissionSelection` Compiler Contract In Tests

**Files:**
- Modify: `internal/adapter/claude/translator_test.go`
- Modify: `internal/adapter/claude/permission_mode_test.go`

- [ ] **Step 1: Write the failing scoped-rules translator test**

```go
func TestClaudeTranslatorPlanConfirmationScopedSessionSelectionBuildsSessionRules(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "plan")

	observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-updates-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-plan-updates-1",
					"name":  "ExitPlanMode",
					"input": map[string]any{},
				},
			},
		},
	})
	observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-plan-updates-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "ExitPlanMode",
			"tool_use_id": "call-plan-updates-1",
			"input": map[string]any{
				"plan": "1. Edit internal/adapter/claude/commands.go\n2. Add docs/general/relay-protocol-spec.md note",
			},
		},
	})

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-plan-updates-1",
			Response: map[string]any{
				"decision": "accept",
				"permissionSelection": map[string]any{
					"scope":       "session",
					"grant_level": "scoped_rules",
					"directories": []any{
						"/data/dl/droid/internal/adapter/claude",
						"/data/dl/droid/docs/general",
					},
					"rule_classes": []any{
						"edit_existing_files",
						"create_new_files",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("translate plan scoped permissions: %v", err)
	}
	frame := decodeFrame(t, payloads[0])
	response := testMapValue(testMapValue(frame["response"])["response"])
	if response["behavior"] != "allow" {
		t.Fatalf("unexpected response body: %#v", response)
	}
	updates := testSliceValue(response["updatedPermissions"])
	if len(updates) != 1 {
		t.Fatalf("expected one addRules update, got %#v", updates)
	}
	update := testMapValue(updates[0])
	if update["type"] != "addRules" || update["destination"] != "session" || update["behavior"] != "allow" {
		t.Fatalf("unexpected permission update: %#v", update)
	}
	rules := testSliceValue(update["rules"])
	if len(rules) == 0 {
		t.Fatalf("expected compiled rules, got %#v", update)
	}

	resolved := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-plan-updates-1",
					"content":     "ok",
					"is_error":    false,
				},
			},
		},
		"tool_use_result": map[string]any{"text": "ok"},
	})
	if len(resolved.Events) != 1 || resolved.Events[0].Kind != agentproto.EventRequestResolved || resolved.Events[0].ThreadID != threadID || resolved.Events[0].TurnID != turnID {
		t.Fatalf("unexpected resolved event: %#v", resolved.Events)
	}
}
```

- [ ] **Step 2: Write the failing `acceptEdits` fast-path test**

```go
func TestClaudeTranslatorPlanConfirmationAggressiveWorkspaceSelectionUsesSessionAcceptEdits(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeTurn(t, tr, "plan")

	observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-accept-edits-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-plan-accept-edits-1",
					"name":  "ExitPlanMode",
					"input": map[string]any{},
				},
			},
		},
	})
	observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-plan-accept-edits-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "ExitPlanMode",
			"tool_use_id": "call-plan-accept-edits-1",
			"input": map[string]any{
				"plan": "1. Update files across the workspace\n2. Move files\n3. Delete obsolete files",
			},
		},
	})

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-plan-accept-edits-1",
			Response: map[string]any{
				"decision": "accept",
				"permissionSelection": map[string]any{
					"scope":       "session",
					"grant_level": "session_file_edits_and_fs_ops",
					"directories": []any{"/data/dl/droid"},
					"rule_classes": []any{
						"edit_existing_files",
						"create_new_files",
						"rename_or_move_files",
						"delete_plan_files",
						"run_common_fs_commands",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("translate plan session acceptEdits: %v", err)
	}
	updates := testSliceValue(testMapValue(testMapValue(decodeFrame(t, payloads[0])["response"])["response"])["updatedPermissions"])
	if len(updates) != 1 {
		t.Fatalf("expected single setMode update, got %#v", updates)
	}
	update := testMapValue(updates[0])
	if update["type"] != "setMode" || update["mode"] != "acceptEdits" || update["destination"] != "session" {
		t.Fatalf("unexpected acceptEdits update: %#v", update)
	}
}
```

- [ ] **Step 3: Write the failing native-mode projection test**

```go
func TestClaudePermissionSelectionFromNativeProjectsAcceptEdits(t *testing.T) {
	selection := claudePermissionSelectionFromNative("acceptEdits")
	if selection.NativeMode != "acceptEdits" {
		t.Fatalf("expected native acceptEdits, got %#v", selection)
	}
	if selection.AccessMode != agentproto.AccessModeAcceptEdits || selection.PlanMode != "off" {
		t.Fatalf("unexpected projected selection: %#v", selection)
	}
}
```

- [ ] **Step 4: Run the focused translator tests to verify they fail**

Run:

```bash
go test ./internal/adapter/claude -run 'TestClaudeTranslatorPlanConfirmationScopedSessionSelectionBuildsSessionRules|TestClaudeTranslatorPlanConfirmationAggressiveWorkspaceSelectionUsesSessionAcceptEdits|TestClaudePermissionSelectionFromNativeProjectsAcceptEdits' -count=1
```

Expected: FAIL because the translator still hardcodes `updatedPermissions=[]` and native `acceptEdits` still projects incorrectly.

- [ ] **Step 5: Commit after the compiler lands**

```bash
git add internal/adapter/claude/translator_test.go internal/adapter/claude/permission_mode_test.go
git commit -m "test: cover claude plan permission compilation"
```

### Task 2: Implement A Session-Scoped Permission Compiler

**Files:**
- Create: `internal/adapter/claude/permission_updates.go`
- Modify: `internal/adapter/claude/commands.go`

- [ ] **Step 1: Add a translator-local compiler file**

```go
type planPermissionSelection struct {
	Scope       string
	GrantLevel  string
	Directories []string
	RuleClasses []string
}

type compiledPlanPermissionSelection struct {
	UpdatedPermissions []any
	FeedbackSuffix     string
}
```

Include helpers that:

- parse `response["permissionSelection"]`
- require `scope == "session"`
- normalize and dedupe directory/rule-class lists
- detect whether the selection includes the current workspace root
- emit native `PermissionUpdate` objects with `destination: "session"`

- [ ] **Step 2: Compile conservative additive rules first**

Use path-scoped rule builders for the safe classes:

```go
func compileScopedPlanPermissionRules(selection planPermissionSelection, cwd string) []map[string]any {
	// edit_existing_files -> Edit(path)
	// create_new_files -> Write(path)
	// additional directories outside cwd -> addDirectories(session)
}
```

Use Claude rule objects shaped like:

```go
map[string]any{
	"type":        "addRules",
	"behavior":    "allow",
	"destination": "session",
	"rules": []any{
		map[string]any{"toolName": "Edit", "ruleContent": "./internal/adapter/claude/**"},
		map[string]any{"toolName": "Write", "ruleContent": "./docs/general/**"},
	},
}
```

- [ ] **Step 3: Add the narrow `acceptEdits` fast path**

Only emit:

```go
map[string]any{
	"type":        "setMode",
	"mode":        "acceptEdits",
	"destination": "session",
}
```

when all of these are true:

- `grant_level == "session_file_edits_and_fs_ops"`
- the selected directories collapse to the whole current workspace
- the selected rule classes already cover:
  - `edit_existing_files`
  - `create_new_files`
  - `rename_or_move_files`
  - `delete_plan_files`
  - `run_common_fs_commands`

Otherwise stay on additive rules only.

- [ ] **Step 4: Fail closed on unsafe breadth without rejecting the whole approval**

When the user selected aggressive rule classes that cannot be safely represented for subdirectory-only scope, do not emit a widening native mode. Instead:

- keep the safe additive file rules
- leave unsupported filesystem operations ungranted
- append a feedback suffix so Claude knows the grant was intentionally narrowed

```go
suffix := "Session auto-grants were narrowed to path-scoped file edits/creates; move/delete/common filesystem commands still require approval when requested."
```

- [ ] **Step 5: Wire the compiler into `buildRequestResponsePayload(...)`**

Replace the current hardcoded:

```go
body["updatedPermissions"] = []any{}
```

with:

```go
updates, feedbackSuffix, err := t.planConfirmationUpdatedPermissions(request, response.Response)
if err != nil {
	return nil, err
}
body["updatedPermissions"] = updates
updatedInput["feedback"] = firstNonEmptyString(
	lookupStringFromAny(response.Response["feedback"]),
	"Approved. Execute the plan.",
)
if feedbackSuffix != "" {
	updatedInput["feedback"] += " " + feedbackSuffix
}
```

- [ ] **Step 6: Run the focused Claude translator tests**

Run:

```bash
go test ./internal/adapter/claude -run 'TestClaudeTranslatorPlanConfirmationScopedSessionSelectionBuildsSessionRules|TestClaudeTranslatorPlanConfirmationAggressiveWorkspaceSelectionUsesSessionAcceptEdits|TestClaudeTranslatorPlanReviseDoesNotInterrupt|TestClaudeTranslatorPlanRequestUsesControlRequestPlan' -count=1
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/claude/permission_updates.go internal/adapter/claude/commands.go internal/adapter/claude/translator_test.go
git commit -m "feat: compile plan permission selections for claude"
```

### Task 3: Keep Native Permission Projection Honest

**Files:**
- Modify: `internal/adapter/claude/permission_mode.go`
- Modify: `internal/claudesessionstore/permission_mode.go`
- Modify: `internal/core/agentproto/access.go`
- Modify: `internal/adapter/claude/commands_permission_mode_test.go`
- Modify: `internal/adapter/claude/permission_mode_test.go`

- [ ] **Step 1: Add a local observed access mode for `acceptEdits`**

```go
const (
	AccessModeFullAccess = "full_access"
	AccessModeConfirm    = "confirm"
	AccessModeAcceptEdits = "accept_edits"
)
```

Normalize native/observed variants:

```go
case "acceptedits", "accept_edits", "accept-edits":
	return AccessModeAcceptEdits
```

Keep helper behavior conservative:

- `ApprovalPolicyForAccessMode("accept_edits")` -> same fallback as confirm
- `ThreadSandboxForAccessMode("accept_edits")` -> same fallback as confirm
- display helpers return `accept edits`

- [ ] **Step 2: Project native `acceptEdits` to the new local mode**

Update both Claude translator and session-store projection helpers so:

```go
case "acceptEdits":
	return claudePermissionSelection{
		NativeMode: "acceptEdits",
		AccessMode: agentproto.AccessModeAcceptEdits,
		PlanMode:   "off",
	}
```

- [ ] **Step 3: Run the permission-mode test slice**

Run:

```bash
go test ./internal/adapter/claude -run 'TestClaudePermissionSelectionFromNativeProjectsAcceptEdits|TestRuntimeStateSnapshotProjectsNativePermissionMode|TestClaudePermissionControlResponseRefreshesObservedConfig' -count=1
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/adapter/claude/permission_mode.go internal/claudesessionstore/permission_mode.go internal/core/agentproto/access.go internal/adapter/claude/commands_permission_mode_test.go internal/adapter/claude/permission_mode_test.go
git commit -m "feat: project native acceptEdits mode locally"
```

### Task 4: Sync Docs And Finish The Issue

**Files:**
- Modify: `docs/general/relay-protocol-spec.md`
- Modify: `docs/inprogress/claude-plan-confirmation-permission-panel-design.md`

- [ ] **Step 1: Update the relay protocol spec**

Document that:

- `permissionSelection` is now compiled in the Claude translator
- all emitted native `PermissionUpdate` entries use `destination: "session"`
- `acceptEdits` is only used for the narrow whole-workspace + full-rule-set fast path
- narrower directory selections fall back to additive rules and may intentionally leave risky filesystem ops on prompt-on-demand

- [ ] **Step 2: Refresh the design doc’s native-facts section**

Add the confirmed SDK detail that `PermissionUpdate.destination` supports:

- `session`
- `localSettings`
- `projectSettings`
- `userSettings`

and note that `#666` intentionally targets only `session`.

- [ ] **Step 3: Run the focused validation set**

Run:

```bash
go test ./internal/adapter/claude -count=1
go test ./internal/core/orchestrator -count=1
bash scripts/check/go-file-length.sh
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add docs/general/relay-protocol-spec.md docs/inprogress/claude-plan-confirmation-permission-panel-design.md
git commit -m "docs: sync claude plan permission bridge contract"
```

