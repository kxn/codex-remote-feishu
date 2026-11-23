package workruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateNativeVerbosityAndOutcome(t *testing.T) {
	for _, tc := range []struct {
		mode, status    string
		present, absent []string
	}{
		{mode: "quiet", status: "completed", present: []string{"stage-final"}, absent: []string{"stage-commentary", "plan-check", "go test", "public-one", "approval/read"}},
		{mode: "normal", status: "completed", present: []string{"stage-final", "stage-commentary", "plan-check", "approval/read"}, absent: []string{"go test", "public-one"}},
		{mode: "verbose", status: "failed", present: []string{"stage-final", "stage-commentary", "plan-check", "go test", "approval/read", "公开的失败原因", "public-two"}, absent: []string{"public-one"}},
		{mode: "chatty", status: "interrupted", present: []string{"stage-commentary", "plan-check", "go test", "approval/read", "public-one", "public-two", "本轮执行已停止"}, absent: []string{"stage-final"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.coordinator.options.EnqueueReconcile = func(string) {}
			setPrivateDisplayMode(f, tc.mode)
			f.start()
			frame := func(e agentproto.Event) {
				t.Helper()
				e.ThreadID, e.TurnID = "session", "turn-1"
				f.now = f.now.Add(time.Second)
				if err := f.handle(e); err != nil {
					t.Fatal(err)
				}
			}
			frame(agentproto.Event{Kind: agentproto.EventItemCompleted, ItemKind: "agent_message", ItemID: "commentary", Metadata: map[string]any{"text": "stage-commentary", "phase": "commentary"}})
			frame(agentproto.Event{Kind: agentproto.EventTurnPlanUpdated, PlanSnapshot: &agentproto.TurnPlanSnapshot{Steps: []agentproto.TurnPlanStep{{Step: "plan-check", Status: agentproto.TurnPlanStepStatusCompleted}}}})
			frame(agentproto.Event{Kind: agentproto.EventItemStarted, ItemKind: "command_execution", ItemID: "shell", Metadata: map[string]any{"command": "go test redacted-command-opaque-argument", "output": "redacted-shell-output"}})
			frame(agentproto.Event{Kind: agentproto.EventItemCompleted, ItemKind: "mcp_tool_call", ItemID: "mcp", Metadata: map[string]any{"server": "approval", "tool": "read", "arguments": "redacted-mcp-arg"}})
			frame(agentproto.Event{Kind: agentproto.EventItemCompleted, ItemKind: "reasoning_summary", ItemID: "public-1", Metadata: map[string]any{"publicSummary": "public-one", "content": "redacted-reasoning"}})
			frame(agentproto.Event{Kind: agentproto.EventItemCompleted, ItemKind: "reasoning_summary", ItemID: "public-2", Metadata: map[string]any{"publicSummary": "public-two"}})
			frame(agentproto.Event{Kind: agentproto.EventItemCompleted, ItemKind: "agent_message", ItemID: "final", Metadata: map[string]any{"text": "stage-final", "phase": "final_answer"}})
			terminal := agentproto.Event{Kind: agentproto.EventTurnCompleted, Status: tc.status}
			if tc.status == "failed" {
				terminal.Problem = &agentproto.ErrorInfo{Code: "wrapper_test_failure", Message: "公开的失败原因", Details: "redacted-failure-detail"}
			}
			frame(terminal)
			sdk := startPrivateSDK(t, false)
			pub, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
			if err != nil {
				t.Fatal(err)
			}
			drainPrivatePublisher(t, pub)
			var published strings.Builder
			for _, req := range sdk.requests() {
				published.WriteString(req.body + "\n")
			}
			for _, value := range tc.present {
				if !strings.Contains(published.String(), value) {
					t.Errorf("missing %q from %s output: %s", value, tc.mode, published.String())
				}
			}
			for _, value := range append(tc.absent, "redacted-") {
				if strings.Contains(published.String(), value) {
					t.Errorf("unexpected %q in %s output", value, tc.mode)
				}
			}
		})
	}
}

func setPrivateDisplayMode(f *fixture, mode string) {
	f.t.Helper()
	f.tx(func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		if attempt.State != work.AttemptRunning {
			expected := attempt.Revision
			attempt.State, attempt.Revision = work.AttemptRunning, expected+1
			if err := tx.PutAttempt(attempt, expected); err != nil {
				return err
			}
		}
		old, err := tx.GetContextPreferences("context")
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		action := workstore.PrivateActionSource{PersonalScopeID: "scope", RunRef: attempt.RunRef, MessageID: "private-human-input"}
		scope, source, err := tx.ValidatePrivateAction(action)
		if err != nil {
			return err
		}
		return tx.PutContextPreferences(work.ContextPreferences{ContextID: "context", ScopeID: "scope", Verbose: &mode, Source: source, UpdatedBy: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Revision: old.Revision + 1, UpdatedAt: f.now}, old.Revision, action)
	})
}

func TestPrivateUnknownProgressPatchDoesNotReplayOrLoseFinal(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	sdk := startPrivateSDK(t, true)
	pub, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	f.start()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnPlanUpdated, ThreadID: "session", TurnID: "turn-1", PlanSnapshot: &agentproto.TurnPlanSnapshot{Explanation: "initial plan"}}); err != nil {
		t.Fatal(err)
	}
	drainPrivatePublisher(t, pub)
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnPlanUpdated, ThreadID: "session", TurnID: "turn-1", PlanSnapshot: &agentproto.TurnPlanSnapshot{Explanation: "will fail PATCH"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pub.RunOnce(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	if requests := sdk.requests(); len(requests) != 2 || requests[1].method != "PATCH" {
		t.Fatalf("expected one attempted PATCH, got %+v", requests)
	}
	for _, event := range []agentproto.Event{
		{Kind: agentproto.EventItemCompleted, ItemKind: "agent_message", ItemID: "answer", Metadata: map[string]any{"text": "still publish the final"}},
		{Kind: agentproto.EventTurnCompleted, Status: "completed"},
	} {
		event.ThreadID, event.TurnID = "session", "turn-1"
		if err := f.handle(event); err != nil {
			t.Fatal(err)
		}
	}
	drainPrivatePublisher(t, pub)
	f.tx(func(tx *workstore.Tx) error {
		items, err := tx.ListOutbox("scope")
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.State == work.OutboxUnknown {
				return nil
			}
		}
		return fmt.Errorf("an unacknowledged PATCH lost its original unknown state")
	})
	requests := sdk.requests()
	if len(requests) != 3 || !strings.Contains(requests[2].body, "still publish the final") {
		t.Fatalf("the original PATCH must remain unknown and final must reach the user: %+v", requests)
	}
}

func TestPrivateNativeModeChangeAppliesToFutureEvents(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	sdk := startPrivateSDK(t, false)
	pub, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	f.start()
	native := func(kind agentproto.EventKind, itemKind, id string, values map[string]any) {
		t.Helper()
		f.now = f.now.Add(time.Second)
		if err := f.handle(agentproto.Event{Kind: kind, ThreadID: "session", TurnID: "turn-1", ItemKind: itemKind, ItemID: id, Metadata: values}); err != nil {
			t.Fatal(err)
		}
		drainPrivatePublisher(t, pub)
	}
	native(agentproto.EventItemStarted, "command_execution", "hidden-before", map[string]any{"command": "git status"})
	setPrivateDisplayMode(f, "verbose")
	native(agentproto.EventItemStarted, "command_execution", "displayed", map[string]any{"command": "go build"})
	setPrivateDisplayMode(f, "quiet")
	native(agentproto.EventItemStarted, "command_execution", "hidden-after", map[string]any{"command": "rg hidden-search"})
	native(agentproto.EventItemCompleted, "agent_message", "final", map[string]any{"text": "changed verbosity and still got the answer"})
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	drainPrivatePublisher(t, pub)
	requests := sdk.requests()
	var shown string
	for _, req := range requests {
		shown += req.body
	}
	if strings.Contains(shown, "git status") || strings.Contains(shown, "hidden-search") || !strings.Contains(shown, "go build") ||
		!strings.Contains(requests[len(requests)-2].body, "go build") || !strings.Contains(requests[len(requests)-1].body, "still got the answer") {
		t.Fatalf("verbosity change removed visible activity or exposed hidden future activity: %s", shown)
	}
}

func TestPrivatePlanCreatesProgressBeforeToolAndFinishesIt(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	sdk := startPrivateSDK(t, false)
	pub, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	f.start()
	drainPrivatePublisher(t, pub)
	if len(sdk.requests()) != 0 {
		t.Fatal("turn start published an empty progress card")
	}
	for _, event := range []agentproto.Event{
		{Kind: agentproto.EventTurnPlanUpdated, PlanSnapshot: &agentproto.TurnPlanSnapshot{Steps: []agentproto.TurnPlanStep{{Step: "inspect inputs", Status: agentproto.TurnPlanStepStatusInProgress}}}},
		{Kind: agentproto.EventItemStarted, ItemKind: "mcp_tool_call", ItemID: "tool", Metadata: map[string]any{"server": "example", "tool": "read"}},
	} {
		event.ThreadID, event.TurnID = "session", "turn-1"
		if err := f.handle(event); err != nil {
			t.Fatal(err)
		}
		drainPrivatePublisher(t, pub)
	}
	requests := sdk.requests()
	if len(requests) == 0 || !strings.Contains(requests[0].body, "inspect inputs") {
		t.Fatalf("plan missing: %+v", requests)
	}
	privateFinalEvent(f, "final answer")
	if err := completePrivateTurn(f); err != nil {
		t.Fatal(err)
	}
	drainPrivatePublisher(t, pub)
	requests = sdk.requests()
	if len(requests) != 3 || !strings.Contains(requests[1].body, "example/read") || !strings.Contains(requests[2].body, "final answer") {
		t.Fatalf("incorrect progress terminal state: %+v", requests)
	}
}
