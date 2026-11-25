package agentproto

// PrivateSessionReadCommand identifies commands whose thread-scoped native
// operation reads session state without claiming an unmanaged session.
func PrivateSessionReadCommand(kind CommandKind) bool {
	switch kind {
	case CommandThreadRead, CommandThreadHistoryRead, CommandThreadGoalGet, CommandMCPOAuthLogin:
		return true
	}
	return false
}
