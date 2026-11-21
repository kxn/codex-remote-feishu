package codex

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestManagedSteerRequiresExactNativeRPCResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body, status string
		kind               agentproto.EventKind
	}{
		{"accepted", `"result":{"turnId":"turn"}`, "accepted", agentproto.EventNativeTurnInputAccepted},
		{"rejected", `"error":{"code":-1,"message":"turn stopped"}`, "failed", agentproto.EventNativeTurnInputRejected},
		{"missing", `"result":{}`, "unknown", agentproto.EventNativeTurnInputRejected},
		{"different", `"result":{"turnId":"other"}`, "unknown", agentproto.EventNativeTurnInputRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewTranslator("instance")
			cmd := agentproto.Command{CommandID: "steer", Kind: agentproto.CommandTurnSteer, NativeInputAdmission: &agentproto.NativeInputAdmissionCommand{Version: 1, ID: "admission", InputIDs: []string{"late"}}}
			cmd.Target.ThreadID = "session"
			cmd.Target.TurnID = "turn"
			frames, err := tr.TranslateCommand(cmd)
			if err != nil {
				t.Fatal(err)
			}
			var frame struct {
				ID string `json:"id"`
			}
			if err = json.Unmarshal(frames[0], &frame); err != nil {
				t.Fatal(err)
			}
			result, err := tr.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,%s}`, frame.ID, tc.body)))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Events) != 1 {
				t.Fatalf("events: %+v", result.Events)
			}
			e := result.Events[0]
			if e.Kind != tc.kind || e.Status != tc.status || e.CommandID != "steer" || e.RequestID != frame.ID || e.ThreadID != "session" || e.TurnID != "turn" {
				t.Fatalf("lost exact receipt: %+v", e)
			}
		})
	}
}
