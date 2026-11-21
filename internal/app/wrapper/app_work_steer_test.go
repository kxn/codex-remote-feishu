package wrapper

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestWorkSteerRequiresExactOwnedActiveTurn(t *testing.T) {
	app := newTestWorkApp(t, agentproto.BackendCodex)
	cmd := testWorkCommand(app)
	cmd.Kind = agentproto.CommandTurnSteer
	cmd.Target.ThreadID = "session"
	cmd.Target.TurnID = "turn"
	cmd.NativeInputAdmission = &agentproto.NativeInputAdmissionCommand{Version: 1, ID: "admission", InputIDs: []string{"late-input"}}
	if _, err := app.prepareWorkCommand(cmd); err == nil {
		t.Fatal("unproven active turn accepted")
	}
	app.work.inspection.turns[nativeTurnKey("session", "turn")] = inspectedTurn{Active: true, Binding: *cmd.Execution}
	if _, err := app.prepareWorkCommand(cmd); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []string{"terminal", "different-attempt", "wrong-version", "no-revision", "duplicate-input", "other-backend"} {
		t.Run(tc, func(t *testing.T) {
			changed := cmd
			binding := *cmd.Execution
			changed.Execution = &binding
			metadata := *cmd.NativeInputAdmission
			changed.NativeInputAdmission = &metadata
			proof := inspectedTurn{Active: true, Binding: *cmd.Execution}
			backend := agentproto.BackendCodex
			switch tc {
			case "terminal":
				proof.Terminal = true
			case "different-attempt":
				binding.AttemptID = "another"
			case "wrong-version":
				metadata.Version = 2
			case "no-revision":
				changed.InstructionRevision = ""
			case "duplicate-input":
				metadata.InputIDs = []string{"late-input", "late-input"}
			case "other-backend":
				backend = agentproto.BackendClaude
			}
			app.work.inspection.turns[nativeTurnKey("session", "turn")] = proof
			if err := app.work.validateNativeSteer(changed, backend); err == nil {
				t.Fatal("invalid steer accepted")
			}
		})
	}
}
