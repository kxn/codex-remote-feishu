package wrapper

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (w *workExecutionState) validateNativeSteer(command agentproto.Command, backend agentproto.Backend) error {
	admission := command.NativeInputAdmission
	if (backend != agentproto.BackendCodex && backend != agentproto.BackendClaude) || admission == nil || admission.Version != agentproto.NativeInputAdmissionProtocolVersion || admission.ID == "" || len(admission.InputIDs) == 0 || command.InstructionRevision != w.snapshot.InstructionBundle.Revision || command.Target.ThreadID == "" || command.Target.TurnID == "" {
		return fmt.Errorf("native steering requires an explicit Codex/Claude input admission and frozen instructions")
	}
	seen := map[string]bool{}
	for _, id := range admission.InputIDs {
		if id == "" || seen[id] {
			return fmt.Errorf("native steering input IDs must be explicit and unique")
		}
		seen[id] = true
	}
	w.inspection.mu.Lock()
	proof, known := w.inspection.turns[nativeTurnKey(command.Target.ThreadID, command.Target.TurnID)]
	w.inspection.mu.Unlock()
	if !known || !proof.Active || proof.Terminal || command.Execution == nil || proof.Binding != *command.Execution {
		return fmt.Errorf("native steering target has no exact active-turn execution proof")
	}
	return nil
}
