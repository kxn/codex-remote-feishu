package orchestrator

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexNativeDefaultReachesWireWithoutFallback(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
			svc := newReplyAutoSteerServiceFixture(&now)
			surface := svc.root.Surfaces["surface-1"]
			surface.CodexConnectionContract = &state.CodexConnectionContract{ConnectionContractID: "native", Kind: state.CodexProfileKindNative}
			surface.CodexThreadPolicy = &state.CodexThreadPolicy{ThreadPolicyID: "native-default", ModelMode: state.CodexThreadValueDefault, ReasoningMode: state.CodexThreadValueDefault, ReviewModelMode: state.CodexReviewModelConfig}
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: surface.SurfaceSessionID})
			text := "continue"
			if explicit {
				text = "[sol:high] continue"
			}
			events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, MessageID: "msg-native", Text: text})
			command := firstCodexCommand(events, agentproto.CommandPromptSend)
			if command == nil {
				t.Fatalf("no prompt command: %#v", events)
			}
			tr := codex.NewTranslator("inst-1")
			frames, err := tr.TranslateCommand(*command)
			if err != nil || len(frames) != 1 {
				t.Fatalf("translate: %v frames=%d", err, len(frames))
			}
			var start map[string]any
			if err := json.Unmarshal(frames[0], &start); err != nil {
				t.Fatal(err)
			}
			params := start["params"].(map[string]any)
			config, _ := params["config"].(map[string]any)
			if explicit {
				if params["model"] != "gpt-6-sol" || config["model_reasoning_effort"] != "high" {
					t.Fatalf("explicit prefix lost at native wire: %#v", params)
				}
			} else {
				if value := params["model"]; value != nil {
					t.Fatalf("invented model override: %#v", params)
				}
				if value := config["model_reasoning_effort"]; value != nil {
					t.Fatalf("invented reasoning override: %#v", config)
				}
			}
			// The native runtime supplies its configured defaults in thread/start. They
			// must survive the queued follow-up turn/start without Remote replacing them.
			response, _ := json.Marshal(map[string]any{"id": start["id"], "result": map[string]any{"thread": map[string]any{"id": "thread-created"}, "model": "gpt-6-sol", "reasoningEffort": "high"}})
			observed, err := tr.ObserveServer(response)
			if err != nil || len(observed.OutboundToCodex) != 1 {
				t.Fatalf("native follow-up: %v %#v", err, observed)
			}
			var turn map[string]any
			if err := json.Unmarshal(observed.OutboundToCodex[0], &turn); err != nil {
				t.Fatal(err)
			}
			turnParams := turn["params"].(map[string]any)
			if got := turnParams["model"]; got != nil && got != "gpt-6-sol" {
				t.Fatalf("native default overwritten: %#v", turnParams)
			}
			if got := turnParams["effort"]; got != nil && got != "high" {
				t.Fatalf("native reasoning overwritten: %#v", turnParams)
			}
		})
	}
}

func TestAutoWhipFrozenNativeDefaultIgnoresLaterTopicSelection(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	parent := &state.QueueItemRecord{FrozenOverride: state.ModelConfigRecord{AccessMode: agentproto.AccessModeFullAccess}}
	svc.scheduleAutoWhip(surface, parent, "turn-parent", state.AutoWhipReasonIncompleteStop)
	surface.CodexPromptOverride = state.CodexPromptOverrideRecord{Model: "gpt-6-astra", ReasoningEffort: "xhigh"}
	now = now.Add(3 * time.Second)
	svc.maybeDispatchPendingAutoWhip(surface, now)
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.FrozenOverride != parent.FrozenOverride {
		t.Fatalf("auto continuation lost frozen native default: %#v", item)
	}
}
