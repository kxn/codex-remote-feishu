package codex

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

func commandFailureInitiator(command agentproto.Command) agentproto.Initiator {
	if command.Target.InternalHelper {
		return agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper}
	}
	return reviewStartInitiator(command)
}

func correlatedCommandErrorEvent(problem agentproto.ErrorInfo) agentproto.Event {
	event := agentproto.NewSystemErrorEvent(problem)
	event.CommandID = problem.CommandID
	return event
}

func nativePromptRejectedEvent(commandID, requestID, threadID string, origin agentproto.TurnCompletionOrigin, message string, initiator agentproto.Initiator) agentproto.Event {
	return agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		CommandID:            commandID,
		RequestID:            requestID,
		ThreadID:             threadID,
		Status:               "failed",
		ErrorMessage:         message,
		TurnCompletionOrigin: origin,
		Initiator:            initiator,
	}
}
