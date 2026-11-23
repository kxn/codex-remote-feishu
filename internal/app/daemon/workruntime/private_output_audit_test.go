package workruntime

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestQuietPrivateNativeSDKOnlyPublishesImageAndFinal(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	setPrivateDisplayMode(f, "quiet")
	f.start()
	translate := codex.NewTranslator(f.carrier.OriginInstanceID)
	for _, frame := range codexPrivateOutputFrames() {
		f.now = f.now.Add(time.Second)
		observed, err := translate.ObserveServer([]byte(frame))
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range observed.Events {
			if err := f.handle(event); err != nil {
				t.Fatal(err)
			}
		}
	}
	sdk := startPrivateSDK(t, false)
	publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	drainPrivatePublisher(t, publisher)
	requests := sdk.requests()
	if len(requests) != 3 || requests[0].path != "/open-apis/im/v1/images" || requests[1].kind != "image" || requests[2].kind != "interactive" || !strings.Contains(requests[2].body, "Codex reply") {
		t.Fatalf("quiet must publish the actual image and final, without progress cards: %+v", requests)
	}
	for _, request := range requests {
		if request.method == http.MethodPatch || strings.Contains(request.body, "正在处理请求") || strings.Contains(request.body, "Preparing the example") {
			t.Fatalf("quiet leaked process output: %+v", request)
		}
	}
}

func TestPrivateNativeRejectionWithoutSessionPublishesActualFailure(t *testing.T) {
	for _, origin := range []agentproto.TurnCompletionOrigin{
		agentproto.TurnCompletionOriginThreadStartRejected,
		agentproto.TurnCompletionOriginThreadResumeRejected,
		agentproto.TurnCompletionOriginTurnStartRejected,
	} {
		t.Run(string(origin), func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.coordinator.options.EnqueueReconcile = func(string) {}
			translator := codex.NewTranslator(f.carrier.OriginInstanceID)
			command := f.command
			command.Target.ExecutionMode = agentproto.PromptExecutionModeResumeExisting
			if origin == agentproto.TurnCompletionOriginThreadStartRejected {
				command.Target.ThreadID = ""
				command.Target.ExecutionMode = agentproto.PromptExecutionModeStartNew
			}
			outgoing, err := translator.TranslateCommand(command)
			if err != nil || len(outgoing) != 1 {
				t.Fatalf("native command: %v %v", outgoing, err)
			}
			requestID := func(frame []byte) string {
				t.Helper()
				var request struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(frame, &request); err != nil {
					t.Fatal(err)
				}
				if request.ID == "" {
					t.Fatal("native request has no correlation ID")
				}
				return request.ID
			}
			id := requestID(outgoing[0])
			if origin == agentproto.TurnCompletionOriginTurnStartRejected {
				response, _ := json.Marshal(map[string]any{"id": id, "result": map[string]any{}})
				observed, err := translator.ObserveServer(response)
				if err != nil || len(observed.OutboundToCodex) != 1 {
					t.Fatalf("resume before turn: %+v %v", observed, err)
				}
				id = requestID(observed.OutboundToCodex[0])
			}
			response, _ := json.Marshal(map[string]any{"id": id, "error": map[string]any{"message": "原生会话未能建立\nhidden native payload"}})
			observed, err := translator.ObserveServer(response)
			if err != nil || len(observed.Events) != 1 || observed.Events[0].TurnCompletionOrigin != origin {
				t.Fatalf("native rejection: %+v %v", observed, err)
			}
			if err := f.handle(observed.Events[0]); err != nil {
				t.Fatal(err)
			}
			sdk := startPrivateSDK(t, false)
			publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
			if err != nil {
				t.Fatal(err)
			}
			drainPrivatePublisher(t, publisher)
			requests := sdk.requests()
			if len(requests) != 1 || requests[0].kind != "interactive" || !strings.Contains(requests[0].body, "原生会话未能建立") || strings.Contains(requests[0].body, "hidden native payload") || strings.Contains(requests[0].body, "正在处理请求") {
				t.Fatalf("empty-session rejection must show one actual failure: %+v", requests)
			}
		})
	}
}

func TestPrivateNativeHelperOutputDoesNotReachSDK(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	f.start()
	helper := f.command
	helper.CommandID = "private-helper-command"
	helper.Target.InternalHelper, helper.Target.ThreadID, helper.Target.ExecutionMode = true, "", agentproto.PromptExecutionModeStartNew
	f.registerExtra(helper)
	translator := codex.NewTranslator(f.carrier.OriginInstanceID)
	outgoing, err := translator.TranslateCommand(helper)
	if err != nil || len(outgoing) != 1 {
		t.Fatalf("helper command: %v %v", outgoing, err)
	}
	var request struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(outgoing[0], &request); err != nil {
		t.Fatal(err)
	}
	response, _ := json.Marshal(map[string]any{"id": request.ID, "error": map[string]any{"message": "hidden helper pre-turn failure"}})
	observed, err := translator.ObserveServer(response)
	if err != nil || len(observed.Events) != 1 || observed.Events[0].Initiator.Kind != agentproto.InitiatorInternalHelper {
		t.Fatalf("helper rejection: %+v %v", observed, err)
	}
	if err := f.handle(observed.Events[0]); err != nil {
		t.Fatal(err)
	}
	for _, event := range []agentproto.Event{
		{Kind: agentproto.EventItemCompleted, ItemKind: "agent_message", ItemID: "helper-answer", Metadata: map[string]any{"text": "hidden helper answer", "phase": "final_answer"}},
		{Kind: agentproto.EventSystemError, Problem: &agentproto.ErrorInfo{Code: "helper-problem", Message: "hidden helper failure"}},
	} {
		event.ThreadID, event.TurnID = "session", "turn-1"
		event.TrafficClass = agentproto.TrafficClassInternalHelper
		if err := f.handle(event); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range []agentproto.Event{
		{Kind: agentproto.EventItemCompleted, ItemKind: "agent_message", ItemID: "human-answer", Metadata: map[string]any{"text": "actual private answer", "phase": "final_answer"}},
		{Kind: agentproto.EventTurnCompleted, Status: "completed"},
	} {
		event.ThreadID, event.TurnID = "session", "turn-1"
		if err := f.handle(event); err != nil {
			t.Fatal(err)
		}
	}
	sdk := startPrivateSDK(t, false)
	publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	drainPrivatePublisher(t, publisher)
	requests := sdk.requests()
	if len(requests) != 1 || !strings.Contains(requests[len(requests)-1].body, "actual private answer") {
		t.Fatalf("helper changed ordinary private progress/final sequence: %+v", requests)
	}
	for _, request := range requests {
		if strings.Contains(request.body, "hidden helper") {
			t.Fatalf("helper output leaked to SDK: %+v", request)
		}
	}
}

func TestPrivateControlContextOnlyDisplaysActualTerminalFailure(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true, true)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	f.start()
	for _, event := range []agentproto.Event{
		{Kind: agentproto.EventItemStarted, ItemKind: "command_execution", ItemID: "control-tool", Metadata: map[string]any{"command": "hidden control command"}},
		{Kind: agentproto.EventItemCompleted, ItemKind: "agent_message", ItemID: "control-answer", Metadata: map[string]any{"text": "hidden control result", "phase": "final_answer"}},
		{Kind: agentproto.EventTurnCompleted, Status: "failed", ErrorMessage: "The requested model could not be found\nhidden backend detail"},
	} {
		event.ThreadID, event.TurnID = "session", "turn-1"
		if err := f.handle(event); err != nil {
			t.Fatal(err)
		}
	}
	// Duplicate native terminal observations cannot append another failure card.
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "failed", ErrorMessage: "The requested model could not be found"}); err != nil {
		t.Fatal(err)
	}
	sdk := startPrivateSDK(t, false)
	publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	drainPrivatePublisher(t, publisher)
	requests := sdk.requests()
	if len(requests) != 1 || !strings.Contains(requests[0].body, "The requested model could not be found") || strings.Contains(requests[0].body, "hidden") || strings.Contains(requests[0].body, "正在处理请求") {
		t.Fatalf("private control failure must be the only visible output: %+v", requests)
	}
}
