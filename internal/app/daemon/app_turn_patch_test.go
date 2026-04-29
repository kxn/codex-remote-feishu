package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	turnpatchruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/turnpatchruntime"
	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTurnPatchOpenFlowBuildsRequestCard(t *testing.T) {
	app, _, _ := newTurnPatchTestApp(t)

	events, handled := interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionTurnPatchCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/bendtomywill",
	})
	if !handled {
		t.Fatal("expected /bendtomywill to be intercepted")
	}
	if len(events) != 1 || events[0].RequestView == nil {
		t.Fatalf("expected one request view event, got %#v", events)
	}
	view := control.NormalizeFeishuRequestView(*events[0].RequestView)
	if view.Title != "修补当前会话" || len(view.Questions) != 2 {
		t.Fatalf("unexpected patch request view: %#v", view)
	}
	if !strings.Contains(view.Questions[0].Question, "命中片段：") {
		t.Fatalf("expected first question to include excerpt, got %#v", view.Questions[0])
	}
	if !strings.Contains(view.Sections[0].Lines[0], "只会替换当前会话最新一轮助手回复") {
		t.Fatalf("expected request card summary, got %#v", view.Sections)
	}
	flow := mustOnlyTurnPatchFlow(t, app)
	if flow.Stage != turnpatchruntime.FlowStageEditing || flow.ThreadID != "thread-1" || flow.OwnerUserID != "user-1" {
		t.Fatalf("unexpected flow state after open: %#v", flow)
	}
}

func TestTurnPatchOpenRejectsVSCodeAndBusyInstance(t *testing.T) {
	app, _, _ := newTurnPatchTestApp(t)
	surface := app.service.Surface("surface-1")
	surface.ProductMode = state.ProductModeVSCode

	events, handled := interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionTurnPatchCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/bendtomywill",
	})
	if !handled || len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "turn_patch_vscode_unsupported" {
		t.Fatalf("expected vscode reject notice, got %#v", events)
	}

	surface.ProductMode = state.ProductModeNormal
	app.service.Instance("inst-1").ActiveTurnID = "turn-active"
	events, handled = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionTurnPatchCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/bendtomywill",
	})
	if !handled || len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "turn_patch_busy" {
		t.Fatalf("expected busy reject notice, got %#v", events)
	}
}

func TestTurnPatchRequestFlowAdvancesAndRejectsOtherUser(t *testing.T) {
	app, _, _ := newTurnPatchTestApp(t)
	_, _ = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionTurnPatchCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/bendtomywill",
	})
	flow := mustOnlyTurnPatchFlow(t, app)

	firstQuestionID := flow.Candidates[0].QuestionID
	secondQuestionID := flow.Candidates[1].QuestionID
	events, handled := interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-patch-1",
		Request: &control.ActionRequestResponse{
			RequestID:       flow.RequestID,
			RequestType:     "request_user_input",
			RequestRevision: flow.Revision,
			Answers: map[string][]string{
				firstQuestionID: {"patched refusal"},
			},
		},
		RequestAnswers: map[string][]string{
			firstQuestionID: {"patched refusal"},
		},
	})
	if !handled || len(events) != 1 || events[0].RequestView == nil || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected partial answer to refresh current request card, got %#v", events)
	}
	view := control.NormalizeFeishuRequestView(*events[0].RequestView)
	if view.CurrentQuestionIndex != 1 || !view.Questions[0].Answered || view.Questions[0].DefaultValue != "patched refusal" {
		t.Fatalf("expected first answer to advance to second question, got %#v", view)
	}
	if flow.Revision != 2 || flow.Answers[firstQuestionID] != "patched refusal" || flow.Answers[secondQuestionID] != "" {
		t.Fatalf("unexpected stored answers after first step: %#v", flow)
	}

	events, handled = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-2",
		MessageID:        "om-patch-1",
		Request: &control.ActionRequestResponse{
			RequestID:       flow.RequestID,
			RequestType:     "request_user_input",
			RequestRevision: flow.Revision,
			Answers: map[string][]string{
				secondQuestionID: {"patched placeholder"},
			},
		},
		RequestAnswers: map[string][]string{
			secondQuestionID: {"patched placeholder"},
		},
	})
	if !handled || len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "turn_patch_unauthorized" {
		t.Fatalf("expected non-owner reject notice, got %#v", events)
	}
}

func TestTurnPatchApplySuccessWritesRolloutAndRestartsChild(t *testing.T) {
	app, rolloutPath, _ := newTurnPatchTestApp(t)
	_, _ = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionTurnPatchCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/bendtomywill",
	})
	flow := mustOnlyTurnPatchFlow(t, app)
	firstQuestionID := flow.Candidates[0].QuestionID
	secondQuestionID := flow.Candidates[1].QuestionID

	restartCh := make(chan agentproto.Command, 4)
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-1" {
			t.Fatalf("unexpected instance id: %s", instanceID)
		}
		restartCh <- command
		return nil
	}

	_, _ = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-patch-1",
		Request: &control.ActionRequestResponse{
			RequestID:       flow.RequestID,
			RequestType:     "request_user_input",
			RequestRevision: flow.Revision,
			Answers: map[string][]string{
				firstQuestionID: {"patched refusal"},
			},
		},
		RequestAnswers: map[string][]string{
			firstQuestionID: {"patched refusal"},
		},
	})
	events, _ := interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-patch-1",
		Request: &control.ActionRequestResponse{
			RequestID:       flow.RequestID,
			RequestType:     "request_user_input",
			RequestRevision: flow.Revision,
			Answers: map[string][]string{
				secondQuestionID: {"patched placeholder"},
			},
		},
		RequestAnswers: map[string][]string{
			secondQuestionID: {"patched placeholder"},
		},
	})
	if len(events) != 1 || events[0].PageView == nil || events[0].PageView.Title != "正在修补当前会话" {
		t.Fatalf("expected final answer to enter applying state, got %#v", events)
	}

	restart := mustReceiveRestartCommand(t, restartCh)
	if restart.Kind != agentproto.CommandProcessChildRestart {
		t.Fatalf("expected restart command, got %#v", restart)
	}
	app.onCommandAck(context.Background(), "inst-1", agentproto.CommandAck{
		CommandID: restart.CommandID,
		Accepted:  true,
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{
		agentproto.NewChildRestartUpdatedEvent(restart.CommandID, "thread-1", agentproto.ChildRestartStatusSucceeded, nil),
	})

	waitForTurnPatchCondition(t, func() bool {
		return app.turnPatchRuntime.ActiveTx["inst-1"] == nil
	})
	if flow.Stage != turnpatchruntime.FlowStageApplied || strings.TrimSpace(flow.PatchID) == "" || strings.TrimSpace(flow.BackupPath) == "" {
		t.Fatalf("unexpected flow state after apply success: %#v", flow)
	}
	updatedRaw, err := os.ReadFile(rolloutPath)
	if err != nil {
		t.Fatalf("read rollout after apply: %v", err)
	}
	updated := string(updatedRaw)
	if !strings.Contains(updated, "patched refusal") || !strings.Contains(updated, "patched placeholder") {
		t.Fatalf("expected rollout to contain patched texts, got %s", updated)
	}
	if strings.Contains(updated, "\"type\":\"reasoning\"") || strings.Contains(updated, "\"type\":\"agent_reasoning\"") {
		t.Fatalf("expected reasoning lines removed after apply, got %s", updated)
	}
}

func TestTurnPatchApplyAcceptsImmediateRestartAck(t *testing.T) {
	app, rolloutPath, _ := newTurnPatchTestApp(t)
	_, _ = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionTurnPatchCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/bendtomywill",
	})
	flow := mustOnlyTurnPatchFlow(t, app)
	firstQuestionID := flow.Candidates[0].QuestionID
	secondQuestionID := flow.Candidates[1].QuestionID

	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-1" {
			t.Fatalf("unexpected instance id: %s", instanceID)
		}
		app.onCommandAck(context.Background(), "inst-1", agentproto.CommandAck{
			CommandID: command.CommandID,
			Accepted:  true,
		})
		app.onEvents(context.Background(), "inst-1", []agentproto.Event{
			agentproto.NewChildRestartUpdatedEvent(command.CommandID, "thread-1", agentproto.ChildRestartStatusSucceeded, nil),
		})
		return nil
	}

	_, _ = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-patch-1",
		Request: &control.ActionRequestResponse{
			RequestID:       flow.RequestID,
			RequestType:     "request_user_input",
			RequestRevision: flow.Revision,
			Answers: map[string][]string{
				firstQuestionID: {"patched refusal"},
			},
		},
		RequestAnswers: map[string][]string{
			firstQuestionID: {"patched refusal"},
		},
	})
	events, _ := interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-patch-1",
		Request: &control.ActionRequestResponse{
			RequestID:       flow.RequestID,
			RequestType:     "request_user_input",
			RequestRevision: flow.Revision,
			Answers: map[string][]string{
				secondQuestionID: {"patched placeholder"},
			},
		},
		RequestAnswers: map[string][]string{
			secondQuestionID: {"patched placeholder"},
		},
	})
	if len(events) != 1 || events[0].PageView == nil || events[0].PageView.Title != "正在修补当前会话" {
		t.Fatalf("expected final answer to enter applying state, got %#v", events)
	}

	waitForTurnPatchCondition(t, func() bool {
		return app.turnPatchRuntime.ActiveTx["inst-1"] == nil
	})
	if flow.Stage != turnpatchruntime.FlowStageApplied || strings.TrimSpace(flow.PatchID) == "" || strings.TrimSpace(flow.BackupPath) == "" {
		t.Fatalf("unexpected flow state after immediate ack: %#v", flow)
	}
	updatedRaw, err := os.ReadFile(rolloutPath)
	if err != nil {
		t.Fatalf("read rollout after immediate ack: %v", err)
	}
	updated := string(updatedRaw)
	if !strings.Contains(updated, "patched refusal") || !strings.Contains(updated, "patched placeholder") {
		t.Fatalf("expected rollout to contain patched texts, got %s", updated)
	}
}

func TestTurnPatchApplyRestartRejectAutoRollsBack(t *testing.T) {
	app, rolloutPath, originalRaw := newTurnPatchTestApp(t)
	_, _ = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionTurnPatchCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/bendtomywill",
	})
	flow := mustOnlyTurnPatchFlow(t, app)
	firstQuestionID := flow.Candidates[0].QuestionID
	secondQuestionID := flow.Candidates[1].QuestionID

	restartCh := make(chan agentproto.Command, 8)
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		restartCh <- command
		return nil
	}

	_, _ = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-patch-1",
		Request: &control.ActionRequestResponse{
			RequestID:       flow.RequestID,
			RequestType:     "request_user_input",
			RequestRevision: flow.Revision,
			Answers: map[string][]string{
				firstQuestionID: {"patched refusal"},
			},
		},
		RequestAnswers: map[string][]string{
			firstQuestionID: {"patched refusal"},
		},
	})
	_, _ = interceptTurnPatchTestAction(t, app, control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-patch-1",
		Request: &control.ActionRequestResponse{
			RequestID:       flow.RequestID,
			RequestType:     "request_user_input",
			RequestRevision: flow.Revision,
			Answers: map[string][]string{
				secondQuestionID: {"patched placeholder"},
			},
		},
		RequestAnswers: map[string][]string{
			secondQuestionID: {"patched placeholder"},
		},
	})

	firstRestart := mustReceiveRestartCommand(t, restartCh)
	app.onCommandAck(context.Background(), "inst-1", agentproto.CommandAck{
		CommandID: firstRestart.CommandID,
		Accepted:  false,
		Error:     "restore failed",
	})
	secondRestart := mustReceiveRestartCommand(t, restartCh)
	app.onCommandAck(context.Background(), "inst-1", agentproto.CommandAck{
		CommandID: secondRestart.CommandID,
		Accepted:  true,
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{
		agentproto.NewChildRestartUpdatedEvent(secondRestart.CommandID, "thread-1", agentproto.ChildRestartStatusSucceeded, nil),
	})

	waitForTurnPatchCondition(t, func() bool {
		return app.turnPatchRuntime.ActiveTx["inst-1"] == nil
	})
	if flow.Stage != turnpatchruntime.FlowStageFailed {
		t.Fatalf("expected flow to end in failed-after-recovery state, got %#v", flow)
	}
	restoredRaw, err := os.ReadFile(rolloutPath)
	if err != nil {
		t.Fatalf("read rollout after auto rollback: %v", err)
	}
	if string(restoredRaw) != string(originalRaw) {
		t.Fatalf("expected rollout restored after failed restart")
	}
}

func interceptTurnPatchTestAction(t *testing.T, app *App, action control.Action) ([]eventcontract.Event, bool) {
	t.Helper()
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.interceptTurnPatchActionLocked(action)
}

func mustOnlyTurnPatchFlow(t *testing.T, app *App) *turnpatchruntime.FlowRecord {
	t.Helper()
	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.turnPatchRuntime.ActiveFlows) != 1 {
		t.Fatalf("expected exactly one active patch flow, got %#v", app.turnPatchRuntime.ActiveFlows)
	}
	for _, flow := range app.turnPatchRuntime.ActiveFlows {
		return flow
	}
	t.Fatal("missing active patch flow")
	return nil
}

func mustReceiveRestartCommand(t *testing.T, ch <-chan agentproto.Command) agentproto.Command {
	t.Helper()
	select {
	case command := <-ch:
		return command
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restart command")
		return agentproto.Command{}
	}
}

func waitForTurnPatchCondition(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for turn patch condition")
}

func newTurnPatchTestApp(t *testing.T) (*App, string, []byte) {
	t.Helper()
	sessionsRoot := filepath.Join(t.TempDir(), "sessions")
	rolloutPath, originalRaw := writeTurnPatchRolloutFixture(t, sessionsRoot)
	storage, err := codexstate.NewTurnPatchStorage(codexstate.TurnPatchStorageOptions{
		SessionsRoot:  sessionsRoot,
		PatchStateDir: filepath.Join(filepath.Dir(sessionsRoot), "patch-state"),
		Logf:          func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("new turn patch storage: %v", err)
	}
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 26, 16, 0, 0, 0, time.UTC),
	})
	app.SetTurnPatchStorage(storage)
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "atlas",
		WorkspaceRoot: "/tmp/workspace",
		WorkspaceKey:  "/tmp/workspace",
		Source:        "headless",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Loaded: true},
		},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	surface := app.service.Surface("surface-1")
	if surface == nil {
		t.Fatal("expected surface after attach")
	}
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModeFollowLocal
	return app, rolloutPath, originalRaw
}

func writeTurnPatchRolloutFixture(t *testing.T, sessionsRoot string) (string, []byte) {
	t.Helper()
	path := filepath.Join(sessionsRoot, "rollout-thread-1.jsonl")
	lines := []map[string]any{
		{"type": "session_meta", "payload": map[string]any{"id": "thread-1"}},
		{"type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": "turn-1"}},
		{"type": "event_msg", "payload": map[string]any{"type": "agent_message", "phase": "final_answer", "message": "older turn"}},
		{"type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant", "phase": "final_answer", "content": []map[string]any{{"type": "output_text", "text": "older turn"}}}},
		{"type": "event_msg", "payload": map[string]any{"type": "task_complete", "turn_id": "turn-1", "last_agent_message": "older turn"}},
		{"type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": "turn-2"}},
		{"type": "event_msg", "payload": map[string]any{"type": "agent_reasoning", "message": "hidden reasoning"}},
		{"type": "response_item", "payload": map[string]any{"type": "reasoning"}},
		{"type": "event_msg", "payload": map[string]any{"type": "agent_message", "phase": "commentary", "message": "I cannot assist with that request."}},
		{"type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant", "phase": "commentary", "content": []map[string]any{{"type": "output_text", "text": "I cannot assist with that request."}}}},
		{"type": "event_msg", "payload": map[string]any{"type": "agent_message", "phase": "final_answer", "message": "Please provide more context before I continue."}},
		{"type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant", "phase": "final_answer", "content": []map[string]any{{"type": "output_text", "text": "Please provide more context before I continue."}}}},
		{"type": "event_msg", "payload": map[string]any{"type": "task_complete", "turn_id": "turn-2", "last_agent_message": "Please provide more context before I continue."}},
	}
	var raw []byte
	for _, line := range lines {
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("marshal rollout line: %v", err)
		}
		raw = append(raw, encoded...)
		raw = append(raw, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	return path, raw
}
