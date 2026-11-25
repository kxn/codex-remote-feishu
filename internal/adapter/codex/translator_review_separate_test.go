package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestIndependentReviewUsesFrozenConfigurationAndDoesNotAdoptChild(t *testing.T) {
	for _, mode := range []string{agentproto.CodexReviewModelExplicit, agentproto.CodexReviewModelSameAsMain} {
		t.Run(mode, func(t *testing.T) {
			tr := NewTranslator("instance")
			tr.currentThreadID = "parent"
			tr.latestThreadStartParams = map[string]any{"cwd": "/wrong", "model": "other-account", "config": map[string]any{"unrelated": "bad"}}
			command := agentproto.Command{CommandID: "review-command", Kind: agentproto.CommandReviewStart, Origin: agentproto.Origin{Surface: "surface"}, Target: agentproto.Target{ThreadID: "parent", CWD: "/workspace"}, Overrides: agentproto.PromptOverrides{Model: "main-model", ReasoningEffort: "high", AccessMode: "full-access"}, CodexResume: &agentproto.CodexResumePolicy{Mode: agentproto.CodexResumeApplyTargetProfile, ModelProviderID: "provider", ModelMode: agentproto.CodexThreadValueExplicit, Model: "main-model", ReviewModelMode: mode, ReviewModel: "review-model"}, Review: agentproto.ReviewRequest{Delivery: agentproto.ReviewDeliveryDetached, Target: agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges}}}
			frames, err := tr.TranslateCommand(command)
			if err != nil || len(frames) != 1 {
				t.Fatalf("translate: %v", err)
			}
			var frame struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err = json.Unmarshal(frames[0], &frame); err != nil {
				t.Fatal(err)
			}
			config, _ := frame.Params["config"].(map[string]any)
			wantReview := "review-model"
			if mode == agentproto.CodexReviewModelSameAsMain {
				wantReview = "main-model"
			}
			if frame.Method != "thread/start" || frame.Params["cwd"] != "/workspace" || frame.Params["model"] != "main-model" || frame.Params["ephemeral"] != true || frame.Params["approvalPolicy"] != "never" || frame.Params["sandbox"] != "danger-full-access" || config["model_reasoning_effort"] != "high" || config["review_model"] != wantReview || config["unrelated"] != nil {
				t.Fatalf("wrong frozen start: %+v", frame)
			}
			if _, err = tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"child","source":"vscode","cwd":"/workspace"}}}`)); err != nil {
				t.Fatal(err)
			}
			start, err := tr.ObserveServer([]byte(`{"id":"relay-review-thread-start-0","result":{"thread":{"id":"child"},"model":"main-model"}}`))
			if err != nil || len(start.OutboundToCodex) != 1 {
				t.Fatalf("start response: %+v %v", start, err)
			}
			if tr.currentThreadID != "parent" {
				t.Fatal("review notification adopted child as parent")
			}
			if _, err = tr.ObserveServer([]byte(`{"id":"relay-review-start-1","result":{"reviewThreadId":"child","turn":{"id":"review-turn"}}}`)); err != nil {
				t.Fatal(err)
			}
			follow := agentproto.Command{Kind: agentproto.CommandPromptSend, Origin: agentproto.Origin{Surface: "surface"}, Target: agentproto.Target{ExecutionMode: agentproto.PromptExecutionModeResumeExisting, ThreadID: "child", SourceThreadID: "parent", CWD: "/workspace", Purpose: agentproto.PromptPurposeReview, SurfaceBindingPolicy: agentproto.SurfaceBindingPolicyKeepSurfaceSelection}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "follow up"}}}}
			frames, err = tr.TranslateCommand(follow)
			if err != nil || len(frames) != 1 {
				t.Fatalf("follow up: %v", err)
			}
			if !strings.Contains(string(frames[0]), `"method":"turn/start"`) || strings.Contains(string(frames[0]), `"method":"thread/resume"`) {
				t.Fatalf("ephemeral review must use live turn: %s", frames[0])
			}
			if tr.currentThreadID != "parent" {
				t.Fatal("review follow-up adopted child")
			}
		})
	}
}

func TestIndependentReviewFailuresRetainExactCommandAndStage(t *testing.T) {
	for _, stage := range []string{"thread/start", "review/start"} {
		t.Run(stage, func(t *testing.T) {
			tr := NewTranslator("instance")
			command := agentproto.Command{CommandID: "review-command", Kind: agentproto.CommandReviewStart, NativeBinding: &agentproto.NativeTurnBinding{}, Origin: agentproto.Origin{Surface: "surface"}, Target: agentproto.Target{ThreadID: "parent", CWD: "/workspace"}, Review: agentproto.ReviewRequest{Delivery: agentproto.ReviewDeliveryDetached, Target: agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges}}}
			if _, err := tr.TranslateCommand(command); err != nil {
				t.Fatal(err)
			}
			request := "relay-review-thread-start-0"
			if stage == "review/start" {
				if _, err := tr.ObserveServer([]byte(`{"id":"relay-review-thread-start-0","result":{"thread":{"id":"child"}}}`)); err != nil {
					t.Fatal(err)
				}
				request = "relay-review-start-1"
			}
			result, err := tr.ObserveServer([]byte(`{"id":"` + request + `","error":{"code":-32602,"message":"precise failure"}}`))
			if err != nil || len(result.Events) != 1 {
				t.Fatalf("result: %+v %v", result, err)
			}
			event := result.Events[0]
			if event.CommandID != "review-command" || event.RequestID != request || event.ThreadID != "parent" || event.Action != "review.start" || event.Status != "failed" || event.Metadata["stage"] != stage || event.ErrorMessage != "precise failure" {
				t.Fatalf("lost exact failure: %+v", event)
			}
		})
	}
}

func TestIndependentReviewRejectsUnprovenChildWithoutStartingReview(t *testing.T) {
	for _, child := range []string{"", "parent"} {
		tr := NewTranslator("instance")
		cmd := agentproto.Command{CommandID: "cmd", Kind: agentproto.CommandReviewStart, NativeBinding: &agentproto.NativeTurnBinding{}, Origin: agentproto.Origin{Surface: "s"}, Target: agentproto.Target{ThreadID: "parent", CWD: "/workspace"}, Review: agentproto.ReviewRequest{Delivery: agentproto.ReviewDeliveryDetached, Target: agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges}}}
		if _, err := tr.TranslateCommand(cmd); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"id": "relay-review-thread-start-0", "result": map[string]any{"thread": map[string]any{"id": child}}})
		result, err := tr.ObserveServer(raw)
		if err != nil || len(result.OutboundToCodex) != 0 || len(result.Events) != 1 || result.Events[0].Status != "unknown" {
			t.Fatalf("unproven child produced review: %+v %v", result, err)
		}
	}
}

func TestReviewTurnStartedPreservesNativeRootTurnID(t *testing.T) {
	tr := NewTranslator("test")
	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"review-child","turn":{"id":"child-turn","rootTurnId":"review-root"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].TurnID != "child-turn" || result.Events[0].RootTurnID != "review-root" {
		t.Fatalf("native turn identity not preserved: %#v", result.Events)
	}
}

func TestIndependentReviewDoesNotResumeAfterClose(t *testing.T) {
	for _, raw := range []string{
		`{"method":"thread/closed","params":{"threadId":"child"}}`,
		`{"method":"thread/deleted","params":{"threadId":"child"}}`,
		`{"method":"thread/archived","params":{"threadId":"child"}}`,
		`{"method":"thread/status/changed","params":{"threadId":"child","status":{"type":"notLoaded"}}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			tr := NewTranslator("instance")
			tr.pendingReviewThreads["child"] = pendingReviewThread{ParentThreadID: "parent", Independent: true, Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface"}}
			if _, err := tr.ObserveServer([]byte(raw)); err != nil {
				t.Fatal(err)
			}
			cmd := agentproto.Command{Kind: agentproto.CommandPromptSend, Origin: agentproto.Origin{Surface: "surface"}, Target: agentproto.Target{ExecutionMode: agentproto.PromptExecutionModeResumeExisting, ThreadID: "child", SourceThreadID: "parent", Purpose: agentproto.PromptPurposeReview, SurfaceBindingPolicy: agentproto.SurfaceBindingPolicyKeepSurfaceSelection}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "follow"}}}}
			frames, err := tr.TranslateCommand(cmd)
			if err == nil || len(frames) != 0 || !strings.Contains(err.Error(), "已关闭") {
				t.Fatalf("closed review sent: frames=%q error=%v", frames, err)
			}
			if !tr.pendingReviewThreads["child"].Closed || tr.pendingReviewThreads["child"].ParentThreadID != "parent" {
				t.Fatal("review identity lost")
			}
		})
	}
}

func TestThreadHistoryNativeFailurePreservesCommand(t *testing.T) {
	tr := NewTranslator("instance")
	cmd := agentproto.Command{Kind: agentproto.CommandThreadHistoryRead, CommandID: "history-command", Target: agentproto.Target{ThreadID: "thread"}}
	frames, err := tr.TranslateCommand(cmd)
	if err != nil || len(frames) != 1 {
		t.Fatalf("translate: %v", err)
	}
	var req struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(frames[0], &req); err != nil {
		t.Fatal(err)
	}
	result, err := tr.ObserveServer([]byte(`{"id":"` + req.ID + `","error":{"code":-32602,"message":"ephemeral history unavailable"}}`))
	if err != nil || len(result.Events) != 1 {
		t.Fatalf("response: %+v %v", result, err)
	}
	e := result.Events[0]
	if e.Status != "failed" || e.ErrorMessage != "ephemeral history unavailable" || e.CommandID != "history-command" || e.RequestID != req.ID || e.ThreadID != "thread" || e.ThreadHistory != nil {
		t.Fatalf("failure lost: %+v", e)
	}
}
