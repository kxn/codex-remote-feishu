package workstore

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// BindObservedNativeSession establishes the first native session from a proven
// command/turn pair. It does not rebind an occupied carrier or advance its
// generation. Later replacement still goes through PutWorker's strict guards.
func (t *Tx) BindObservedNativeSession(incarnation, sessionID, turnID, commandID string) error {
	turn, err := t.GetNativeTurn(incarnation, sessionID, turnID)
	if err != nil {
		return err
	}
	carrier, err := t.GetCarrier(incarnation)
	if err != nil {
		return err
	}
	worker, err := t.GetWorker(turn.Binding.WorkerSessionID)
	if err != nil {
		return err
	}
	if !carrier.Matches(worker) {
		return nil
	} // historical evidence cannot mutate a new worker.
	if commandID == "" {
		if worker.NativeSessionID == sessionID {
			return nil
		}
		return fmt.Errorf("first native session requires an exact command")
	}
	command, err := t.GetNativeCommand(commandID)
	if err != nil {
		return err
	}
	if command.Incarnation != incarnation || command.Binding != turn.Binding {
		return fmt.Errorf("native session command ownership mismatch")
	}
	if command.Kind != agentproto.CommandPromptSend {
		return nil
	}
	canonical, err := t.NativeCommandPayload(commandID, incarnation)
	if err != nil {
		return err
	}
	mode := canonical.Target.EffectivePromptExecutionMode()
	if mode == agentproto.PromptExecutionModeForkEphemeral || mode == agentproto.PromptExecutionModeStartEphemeral {
		return nil
	}
	if canonical.Target.ThreadID != "" && mode != agentproto.PromptExecutionModeStartNew && canonical.Target.ThreadID != sessionID {
		return fmt.Errorf("native result differs from requested resume session")
	}
	if worker.NativeSessionID != "" {
		if worker.NativeSessionID != sessionID || worker.NativeNamespace != string(carrier.Backend) {
			return fmt.Errorf("native session replacement requires a fresh carrier")
		}
		return nil
	}
	if _, err = t.ValidateNativeBinding(turn.Binding); err != nil {
		return err
	}
	expected := worker.Revision
	worker.Revision++
	worker.NativeSessionID = sessionID
	worker.NativeNamespace = string(carrier.Backend)
	worker.UpdatedAt = turn.UpdatedAt
	if err = worker.Validate(); err != nil {
		return err
	}
	return t.record("workers", worker.ID, worker.Revision, expected, worker)
}
