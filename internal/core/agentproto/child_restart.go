package agentproto

func NewChildRestartUpdatedEvent(commandID, threadID string, status ChildRestartStatus, problem *ErrorInfo) Event {
	event := Event{
		Kind:      EventProcessChildRestartUpdated,
		CommandID: commandID,
		ThreadID:  threadID,
		Status:    string(status),
	}
	if problem == nil {
		return event
	}
	value := problem.WithDefaults(ErrorInfo{
		Code:      "child_restart_restore_failed",
		Layer:     "wrapper",
		Stage:     "restart_child_restore_response",
		Operation: string(CommandProcessChildRestart),
		CommandID: commandID,
		ThreadID:  threadID,
	})
	event.ErrorMessage = value.Message
	event.Problem = &value
	return event
}
