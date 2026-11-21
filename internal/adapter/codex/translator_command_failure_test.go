package codex

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestNativePromptRejectionCorrelatesExactCommandRequest(t *testing.T) {
	for _, stage := range []string{"thread/start", "thread/resume", "turn/start"} {
		t.Run(stage, func(t *testing.T) {
			tr := NewTranslator("inst-1")
			requests := make([]string, 2)
			threads := make([]string, 2)
			for i := range requests {
				target := agentproto.Target{ExecutionMode: agentproto.PromptExecutionModeStartNew, CWD: "/tmp/work"}
				if stage == "thread/resume" {
					target = agentproto.Target{ThreadID: fmt.Sprintf("thread-%d", i), CWD: "/tmp/work"}
					threads[i] = target.ThreadID
				}
				frames, err := tr.TranslateCommand(agentproto.Command{
					CommandID: fmt.Sprintf("cmd-%d", i), Kind: agentproto.CommandPromptSend,
					Origin: agentproto.Origin{Surface: fmt.Sprintf("surface-%d", i)}, Target: target,
					Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "work"}}},
				})
				if err != nil {
					t.Fatal(err)
				}
				payload := decodeSinglePayload(t, frames)
				requests[i] = payload["id"].(string)
				if stage == "turn/start" {
					threads[i] = fmt.Sprintf("native-thread-%d", i)
					result, err := tr.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"thread":{"id":%q}}}`, requests[i], threads[i])))
					if err != nil {
						t.Fatal(err)
					}
					payload = decodeSinglePayload(t, result.OutboundToCodex)
					if payload["method"] != "turn/start" {
						t.Fatalf("missing native followup: %#v", payload)
					}
					requests[i] = payload["id"].(string)
				}
			}
			// Reverse response order: association must follow protocol IDs, never current thread or recency.
			for i := len(requests) - 1; i >= 0; i-- {
				result, err := tr.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"error":{"code":-1,"message":"native rejected"}}`, requests[i])))
				if err != nil || len(result.Events) != 1 || len(result.OutboundToCodex) != 0 {
					t.Fatalf("native rejection = %#v, %v", result, err)
				}
				event := result.Events[0]
				if event.CommandID != fmt.Sprintf("cmd-%d", i) || event.RequestID != requests[i] || event.ThreadID != threads[i] || event.Initiator.SurfaceSessionID != fmt.Sprintf("surface-%d", i) {
					t.Fatalf("wrong command/request/native binding: %#v", event)
				}
				if event.Kind != agentproto.EventTurnCompleted || event.Status != "failed" || event.ErrorMessage != "native rejected" {
					t.Fatalf("wrong terminal failure: %#v", event)
				}
			}
		})
	}
}

func TestCompactRejectionRetainsCommandAndNativeRequest(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) {
			tr := NewTranslator("inst-1")
			if !resume {
				if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1"}}`)); err != nil {
					t.Fatal(err)
				}
			}
			frames, err := tr.TranslateCommand(agentproto.Command{CommandID: "cmd-compact", Kind: agentproto.CommandThreadCompactStart, Target: agentproto.Target{ThreadID: "thread-1"}})
			if err != nil {
				t.Fatal(err)
			}
			payload := decodeSinglePayload(t, frames)
			raw, err := json.Marshal(map[string]any{"id": payload["id"], "error": map[string]any{"message": "rejected"}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := tr.ObserveServer(raw)
			if err != nil || len(result.Events) != 1 {
				t.Fatalf("compact result = %#v, %v", result, err)
			}
			event := result.Events[0]
			if event.CommandID != "cmd-compact" || event.RequestID != payload["id"] || event.ThreadID != "thread-1" {
				t.Fatalf("compact lost command/request binding: %#v", event)
			}
		})
	}
}
