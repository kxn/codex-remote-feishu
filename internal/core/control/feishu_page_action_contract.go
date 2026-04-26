package control

import "strings"

// BuildFeishuActionText builds canonical slash text from a structured action.
// Page-card callbacks should use structured action payloads and derive text
// only at the gateway boundary when reducers still consume text arguments.
func BuildFeishuActionText(kind ActionKind, argument string) string {
	base := canonicalSlashForActionKind(kind)
	if base == "" {
		return ""
	}
	argument = strings.TrimSpace(argument)
	if argument == "" {
		return base
	}
	return base + " " + argument
}

func FeishuActionArgumentText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	parts := strings.Fields(text)
	if len(parts) <= 1 {
		return ""
	}
	return strings.TrimSpace(strings.Join(parts[1:], " "))
}

func ActionKindForFeishuCommandID(commandID string) (ActionKind, bool) {
	if flow, ok := FeishuConfigFlowDefinitionByCommandID(strings.TrimSpace(commandID)); ok {
		return flow.ActionKind, true
	}
	switch strings.TrimSpace(commandID) {
	case FeishuCommandMenu:
		return ActionShowCommandMenu, true
	case FeishuCommandHelp:
		return ActionShowCommandHelp, true
	case FeishuCommandHistory:
		return ActionShowHistory, true
	case FeishuCommandCron:
		return ActionCronCommand, true
	case FeishuCommandUpgrade:
		return ActionUpgradeCommand, true
	case FeishuCommandDebug:
		return ActionDebugCommand, true
	case FeishuCommandVSCodeMigrate:
		return ActionVSCodeMigrateCommand, true
	case FeishuCommandWorkspace:
		return ActionWorkspaceRoot, true
	case FeishuCommandWorkspaceList:
		return ActionWorkspaceList, true
	case FeishuCommandWorkspaceNew:
		return ActionWorkspaceNew, true
	case FeishuCommandWorkspaceNewDir:
		return ActionWorkspaceNewDir, true
	case FeishuCommandWorkspaceNewGit:
		return ActionWorkspaceNewGit, true
	case FeishuCommandWorkspaceNewWorktree:
		return ActionWorkspaceNewWorktree, true
	case FeishuCommandWorkspaceDetach:
		return ActionWorkspaceDetach, true
	case FeishuCommandList:
		return ActionListInstances, true
	case FeishuCommandUse:
		return ActionShowThreads, true
	case FeishuCommandUseAll:
		return ActionShowAllThreads, true
	case FeishuCommandSendFile:
		return ActionSendFile, true
	case FeishuCommandStatus:
		return ActionStatus, true
	default:
		return "", false
	}
}

func canonicalSlashForActionKind(kind ActionKind) string {
	if flow, ok := FeishuConfigFlowDefinitionByActionKind(kind); ok {
		return flow.BareCommand
	}
	switch kind {
	case ActionShowCommandMenu:
		return "/menu"
	case ActionShowCommandHelp:
		return "/help"
	case ActionShowHistory:
		return "/history"
	case ActionCronCommand:
		return "/cron"
	case ActionUpgradeCommand:
		return "/upgrade"
	case ActionDebugCommand:
		return "/debug"
	case ActionVSCodeMigrateCommand:
		return "/vscode-migrate"
	case ActionWorkspaceRoot:
		return "/workspace"
	case ActionWorkspaceList:
		return "/workspace list"
	case ActionWorkspaceNew:
		return "/workspace new"
	case ActionWorkspaceNewDir:
		return "/workspace new dir"
	case ActionWorkspaceNewGit:
		return "/workspace new git"
	case ActionWorkspaceNewWorktree:
		return "/workspace new worktree"
	case ActionWorkspaceDetach:
		return "/workspace detach"
	case ActionListInstances:
		return "/list"
	case ActionShowThreads:
		return "/use"
	case ActionShowAllThreads:
		return "/useall"
	case ActionSendFile:
		return "/sendfile"
	case ActionStatus:
		return "/status"
	case ActionStop:
		return "/stop"
	case ActionCompact:
		return "/compact"
	case ActionSteerAll:
		return "/steerall"
	case ActionNewThread:
		return "/new"
	case ActionDetach:
		return "/detach"
	case ActionFollowLocal:
		return "/follow"
	default:
		return ""
	}
}
