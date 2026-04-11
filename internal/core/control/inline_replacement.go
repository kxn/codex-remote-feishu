package control

import "strings"

// SupportsInlineCardReplacement reports whether this action is a pure
// same-context navigation step that can safely replace the triggering card.
func SupportsInlineCardReplacement(action Action) bool {
	switch action.Kind {
	case ActionShowCommandMenu:
		return true
	case ActionModeCommand:
		return isBareInlineCommand(action.Text, "/mode")
	case ActionAutoContinueCommand:
		return isBareInlineCommand(action.Text, "/autocontinue")
	case ActionReasoningCommand:
		return isBareInlineCommand(action.Text, "/reasoning")
	case ActionAccessCommand:
		return isBareInlineCommand(action.Text, "/access")
	case ActionModelCommand:
		return isBareInlineCommand(action.Text, "/model")
	case ActionShowAllWorkspaces,
		ActionShowRecentWorkspaces,
		ActionShowAllThreadWorkspaces,
		ActionShowRecentThreadWorkspaces:
		return true
	case ActionShowThreads,
		ActionShowAllThreads,
		ActionShowScopedThreads,
		ActionShowWorkspaceThreads:
		return true
	default:
		return false
	}
}

func isBareInlineCommand(text, command string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	return len(fields) == 1 && strings.EqualFold(fields[0], strings.TrimSpace(command))
}
