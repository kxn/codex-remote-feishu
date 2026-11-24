package privatechat

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

// nativeQueryOutcome identifies only responses to the exact command and
// thread; resolveNativeEventTx additionally requires the journaled RPC frame.
func nativeQueryOutcome(command agentproto.Command, event agentproto.Event) (string, string, bool) {
	if command.NativeBinding == nil || command.NativeBinding.Owner.Backend != agentproto.BackendCodex || command.CommandID == "" || event.CommandID != command.CommandID || event.RequestID == "" || command.Target.ThreadID == "" || event.ThreadID != command.Target.ThreadID {
		return "", "", false
	}
	switch command.Kind {
	case agentproto.CommandThreadGoalGet, agentproto.CommandThreadGoalSet, agentproto.CommandThreadGoalClear:
		if event.Kind != agentproto.EventThreadGoalCommandResult || event.ThreadGoal != nil && event.ThreadGoal.ThreadID != command.Target.ThreadID {
			return "", "", false
		}
		method := map[agentproto.CommandKind]string{agentproto.CommandThreadGoalGet: "thread/goal/get", agentproto.CommandThreadGoalSet: "thread/goal/set", agentproto.CommandThreadGoalClear: "thread/goal/clear"}[command.Kind]
		switch event.Status {
		case "accepted":
			return "accepted", method, true
		case "failed":
			return "failed", method, true
		}
	case agentproto.CommandThreadRead:
		if event.Kind != agentproto.EventThreadRuntimeStatusUpdated {
			return "", "", false
		}
		if event.ErrorMessage != "" {
			return "failed", "thread/read", true
		}
		return "accepted", "thread/read", true
	}
	return "", "", false
}
