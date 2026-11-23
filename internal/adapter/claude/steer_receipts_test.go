package claude

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func admittedClaudeSteer(t *testing.T) (*Translator, agentproto.Command) {
	t.Helper()
	tr := NewTranslator("owned-wrapper")
	if _, e := tr.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"session","permissionMode":"default"}`)); e != nil {
		t.Fatal(e)
	}
	first := agentproto.Command{CommandID: "original", Kind: agentproto.CommandPromptSend, Target: agentproto.Target{ThreadID: "session"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "authorized original"}}}}
	if _, e := tr.TranslateCommand(first); e != nil {
		t.Fatal(e)
	}
	tr.RegisterUserCommandUUID("initial-uuid", first)
	if _, e := tr.ObserveServer([]byte(`{"type":"user","uuid":"initial-uuid","session_id":"session","message":{"role":"user","content":[{"type":"text","text":"authorized original"}]}}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := tr.ObserveServer([]byte(`{"type":"assistant","session_id":"session","message":{"id":"working","content":[{"type":"text","text":"working"}]}}`)); e != nil {
		t.Fatal(e)
	}
	if tr.activeTurn == nil || !tr.activeTurn.UserCommandUUIDObserved {
		t.Fatal("initial native echo did not prove actual original input")
	}
	cmd := first
	cmd.CommandID = "supplement"
	cmd.Kind = agentproto.CommandTurnSteer
	cmd.Target.TurnID = tr.activeTurn.TurnID
	cmd.NativeInputAdmission = &agentproto.NativeInputAdmissionCommand{Version: 1, ID: "admission", InputIDs: []string{"image", "caption"}}
	if _, e := tr.TranslateCommand(cmd); e != nil {
		t.Fatal(e)
	}
	tr.RegisterUserCommandUUID("steer-uuid", cmd)
	return tr, cmd
}

func TestAdmittedClaudeSteerRequiresExactNativeUUIDAndActiveTurn(t *testing.T) {
	for _, tc := range []struct {
		name, uuid, session, kind, state string
		want                             string
	}{{"user-echo", "steer-uuid", "session", "user", "", "accepted"}, {"lifecycle", "steer-uuid", "session", "command_lifecycle", "started", "accepted"}, {"queued", "steer-uuid", "session", "command_lifecycle", "queued", ""}, {"foreign-uuid", "other", "session", "user", "", ""}, {"foreign-session", "steer-uuid", "different", "user", "", "unknown"}, {"missing-session", "steer-uuid", "", "user", "", "unknown"}} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cmd := admittedClaudeSteer(t)
			original := tr.activeTurn
			frame := map[string]any{"type": tc.kind, "session_id": tc.session, "uuid": tc.uuid, "command_uuid": tc.uuid, "state": tc.state, "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "supplement"}}}}
			raw, _ := json.Marshal(frame)
			result, e := tr.ObserveServer(raw)
			if e != nil {
				t.Fatal(e)
			}
			got := ""
			for _, event := range result.Events {
				if event.Kind == agentproto.EventNativeTurnInputAccepted || event.Kind == agentproto.EventNativeTurnInputRejected {
					got = event.Status
					if event.CommandID != cmd.CommandID || event.RequestID != "steer-uuid" || event.ThreadID != cmd.Target.ThreadID || event.TurnID != cmd.Target.TurnID {
						t.Fatal("receipt lost exact UUID/target identity")
					}
				}
			}
			if got != tc.want {
				t.Fatalf("native admission status=%s want=%s", got, tc.want)
			}
			if tr.activeTurn != original || tr.activeTurn.CommandID != "original" {
				t.Fatal("supplement replaced original turn ownership")
			}
			if tc.want == "accepted" {
				again, e := tr.ObserveServer(raw)
				if e != nil {
					t.Fatal(e)
				}
				for _, event := range again.Events {
					if event.Kind == agentproto.EventNativeTurnInputAccepted {
						t.Fatal("native duplicate echo accepted twice")
					}
				}
			}
		})
	}
}

func TestAdmittedClaudeSteerLateUUIDDoesNotInventNextTurn(t *testing.T) {
	tr, cmd := admittedClaudeSteer(t)
	if _, e := tr.ObserveServer([]byte(`{"type":"result","subtype":"success","session_id":"session","result":"original finished","is_error":false}`)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"type":"user","uuid":"steer-uuid","session_id":"session","message":{"role":"user","content":[]}}`, `{"type":"command_lifecycle","command_uuid":"steer-uuid","state":"started","session_id":"session"}`} {
		result, e := tr.ObserveServer([]byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, event := range result.Events {
			if event.Kind == agentproto.EventNativeTurnInputAccepted || event.Kind == agentproto.EventTurnStarted {
				t.Fatal("late steer was reported accepted or started another turn")
			}
			if event.Kind == agentproto.EventNativeTurnInputRejected && event.Status == "unknown" && event.TurnID == cmd.Target.TurnID {
				found = true
			}
		}
		if !found || tr.activeTurn != nil || len(tr.pendingTurns) != 0 {
			t.Fatal("uncertain late input did not retain exact original admission")
		}
	}
}

func TestAdmittedClaudeSteerOriginalLifecycleTerminalClosesInputBeforeDelayedResult(t *testing.T) {
	for _, terminal := range []string{"completed", "cancelled"} {
		for _, observed := range []string{`{"type":"user","uuid":"steer-uuid","session_id":"session","message":{"role":"user","content":[]}}`, `{"type":"command_lifecycle","command_uuid":"steer-uuid","state":"started","session_id":"session"}`} {
			t.Run(terminal+observed[:12], func(t *testing.T) {
				tr, command := admittedClaudeSteer(t)
				original := tr.activeTurn
				raw, _ := json.Marshal(map[string]any{"type": "command_lifecycle", "command_uuid": "initial-uuid", "state": terminal, "session_id": "session"})
				if _, e := tr.ObserveServer(raw); e != nil {
					t.Fatal(e)
				}
				if tr.activeTurn != original || !original.NativeInputClosed {
					t.Fatal("terminal lifecycle lost delayed-result owner or left input open")
				}
				result, e := tr.ObserveServer([]byte(observed))
				if e != nil {
					t.Fatal(e)
				}
				unknown := false
				for _, event := range result.Events {
					if event.Kind == agentproto.EventNativeTurnInputAccepted {
						t.Fatal("terminal original accepted a later command")
					}
					unknown = unknown || (event.Kind == agentproto.EventNativeTurnInputRejected && event.Status == "unknown")
				}
				if !unknown {
					t.Fatal("later command result was not retained as unknown")
				}
				if _, e := tr.TranslateCommand(command); e == nil {
					t.Fatal("new steer serialized after native original termination")
				}
				result, e = tr.ObserveServer([]byte(`{"type":"result","subtype":"success","session_id":"session","result":"delayed original final","is_error":false}`))
				if e != nil {
					t.Fatal(e)
				}
				final := false
				for _, event := range result.Events {
					if event.Kind == agentproto.EventTurnCompleted && event.TurnID == original.TurnID {
						final = true
					}
				}
				if !final {
					t.Fatal("delayed original result lost its turn ownership")
				}
			})
		}
	}
}
